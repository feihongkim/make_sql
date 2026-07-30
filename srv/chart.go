package srv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"MakeSQL/console"
)

// 차트 서비스는 pinet 위 컨테이너로 뜬다. 포트를 publish 하지 않으므로
// 호스트에서는 docker exec 로 접근한다. pi·claude 컨테이너는
// http://makesql_chart:8800/chart 를 직접 호출하면 된다.
const (
	chartContainer = "makesql_chart"
	chartPort      = 8800
	chartTimeout   = 180
)

type chartOptions struct {
	source   string
	symbol   string
	from     string
	to       string
	out      string
	title    string
	telegram bool
	boxes    bool
}

// HandleChart 는 chart 서브커맨드를 처리합니다.
//
//	./abledb chart [소스] [종목] --from YYYYMMDD --to YYYYMMDD [옵션]
//
// DB 에서 OHLCV 를 뽑아 캔들차트(이동평균 + 볼린저 밴드)를 그립니다.
// --boxes 를 주면 Box 지지/저항 마커와 MainBox 수평선을 함께 표시합니다.
// 조회·렌더링은 makesql_chart 컨테이너가 하고, 여기서는 요청과 결과 처리만 합니다.
func HandleChart(ctx context.Context, args []string) {
	if len(args) < 2 {
		printChartUsage(ctx)
		return
	}

	opts, err := parseChartArgs(args)
	if err != nil {
		console.LogError("%v", err)
		fmt.Println()
		printChartUsage(ctx)
		os.Exit(1)
	}

	body, err := json.Marshal(map[string]any{
		"source": opts.source,
		"symbol": opts.symbol,
		"from":   opts.from,
		"to":     opts.to,
		"title":  opts.title,
		"boxes":  opts.boxes,
	})
	if err != nil {
		console.LogError("[chart] 요청 생성 실패: %v", err)
		os.Exit(1)
	}

	boxLabel := ""
	if opts.boxes {
		boxLabel = " +box"
	}
	console.Log("[chart] %s / %s / %s~%s%s", opts.source, opts.symbol, opts.from, opts.to, boxLabel)

	png, err := requestChart(ctx, body)
	if err != nil {
		console.LogError("[chart] %v", err)
		os.Exit(1)
	}

	if err := os.WriteFile(opts.out, png, 0644); err != nil {
		console.LogError("[chart] 저장 실패: %v", err)
		os.Exit(1)
	}
	abs, _ := filepath.Abs(opts.out)
	console.Log("[chart] 저장 완료: %s (%.1f KB)", abs, float64(len(png))/1024)
	fmt.Println(abs)

	if opts.telegram {
		caption := fmt.Sprintf("📈 %s %s (%s~%s)%s", opts.source, opts.symbol, opts.from, opts.to, boxLabel)
		if err := SendTelegramPhoto(abs, caption); err != nil {
			console.LogError("[chart] 텔레그램 전송 실패: %v", err)
			os.Exit(1)
		}
		console.Log("[chart] 텔레그램 전송 완료")
	}
}

// requestChart 는 차트 컨테이너에 POST /chart 를 보내고 PNG 를 받습니다.
// 응답이 PNG 가 아니면(오류 JSON) 그 내용을 그대로 드러냅니다.
func requestChart(ctx context.Context, body []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", chartContainer,
		"curl", "-sS", "--max-time", strconv.Itoa(chartTimeout),
		"-X", "POST", fmt.Sprintf("http://127.0.0.1:%d/chart", chartPort),
		"-H", "Content-Type: application/json",
		"--data-binary", "@-",
	)
	cmd.Stdin = bytes.NewReader(body)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("차트 컨테이너 호출 실패: %v\n%s\n컨테이너가 떠 있는지 확인하세요: docker ps | grep %s",
			err, strings.TrimSpace(stderr.String()), chartContainer)
	}

	// PNG 매직 넘버로 판별한다. 오류일 때는 JSON 이 온다.
	if len(out) > 8 && bytes.Equal(out[:4], []byte{0x89, 'P', 'N', 'G'}) {
		return out, nil
	}

	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &e) == nil && e.Error != "" {
		return nil, fmt.Errorf("%s", e.Error)
	}
	return nil, fmt.Errorf("예상치 못한 응답: %s", truncate(string(out), 200))
}

func parseChartArgs(args []string) (chartOptions, error) {
	opts := chartOptions{source: args[0], symbol: args[1]}

	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--from", "--to", "--out", "--title":
			flag := rest[i]
			if i+1 >= len(rest) {
				return opts, fmt.Errorf("%s 값이 없습니다", flag)
			}
			v := rest[i+1]
			switch flag {
			case "--from":
				opts.from = v
			case "--to":
				opts.to = v
			case "--out":
				opts.out = v
			case "--title":
				opts.title = v
			}
			i++
		case "--telegram":
			opts.telegram = true
		case "--boxes":
			opts.boxes = true
		default:
			return opts, fmt.Errorf("알 수 없는 옵션: %s", rest[i])
		}
	}

	// 기간을 안 주면 최근 6개월. 대부분의 확인 작업에 그 정도면 충분하다.
	if opts.to == "" {
		opts.to = time.Now().Format("20060102")
	}
	if opts.from == "" {
		opts.from = time.Now().AddDate(0, -6, 0).Format("20060102")
	}
	parsed := map[string]time.Time{}
	for _, d := range []struct{ name, v string }{{"--from", opts.from}, {"--to", opts.to}} {
		t, err := time.Parse("20060102", d.v)
		if err != nil {
			return opts, fmt.Errorf("%s 는 YYYYMMDD 형식이어야 합니다: %s", d.name, d.v)
		}
		parsed[d.name] = t
	}

	// Box 는 요청 구간 안에서만 계산된다 (RESTGo analyze 의 250일 창과 같은 성질).
	// 구간이 짧으면 Box 가 안 나오거나 다른 구성이 나오는데, 그건 버그가 아니라
	// 입력이 짧은 것이다. 기본 구간(6개월)이 200일 미만이라 그냥 두면 반드시 걸린다.
	if opts.boxes {
		if days := int(parsed["--to"].Sub(parsed["--from"]).Hours() / 24); days < 200 {
			console.Log("[chart] 구간이 %d일입니다 — Box 는 구간 기준으로 계산되므로 200일 이상을 권합니다", days)
		}
	}

	if opts.out == "" {
		os.MkdirAll("test_logs/charts", 0755)
		// box 유무를 파일명에 남긴다. 같은 종목·구간을 양쪽으로 뽑을 때 덮어쓰지 않는다.
		suffix := ""
		if opts.boxes {
			suffix = "_box"
		}
		opts.out = filepath.Join("test_logs/charts",
			fmt.Sprintf("%s_%s_%s-%s%s.png", opts.source, opts.symbol, opts.from, opts.to, suffix))
	}
	return opts, nil
}

func printChartUsage(ctx context.Context) {
	fmt.Println("사용법:")
	fmt.Println("  ./abledb chart [소스] [종목] [옵션]")
	fmt.Println()
	fmt.Println("옵션:")
	fmt.Println("  --from YYYYMMDD  시작일 (기본: 6개월 전)")
	fmt.Println("  --to   YYYYMMDD  종료일 (기본: 오늘)")
	fmt.Println("  --out  경로      저장 위치 (기본: test_logs/charts/)")
	fmt.Println("  --title 제목     차트 제목")
	fmt.Println("  --boxes          Box 마커 ▲지지/▼저항 + MainBox 수평선")
	fmt.Println("  --telegram       완성된 차트를 텔레그램으로 전송")
	fmt.Println()
	fmt.Println("예시:")
	fmt.Println("  ./abledb chart kor_daily 005930")
	fmt.Println("  ./abledb chart kor_daily 005930 --from 20260101 --to 20260720 --telegram")
	fmt.Println("  ./abledb chart ls_daily 000020 --from 20250101 --boxes")
	fmt.Println()
	fmt.Println("캔들 + 이동평균(5/20/60/120) + 볼린저 밴드(20, 2σ)를 그립니다.")
	fmt.Println("--boxes 는 RESTGo box 로직(boxcalc)으로 계산하며, 요청 구간 기준이므로")
	fmt.Println("구간이 달라지면 Box 구성도 달라집니다 (최소 6캔들, 권장 200일 이상).")
	fmt.Println()

	// 등록된 소스는 차트 컨테이너에 물어본다. 목록을 두 곳에 두지 않기 위해서다.
	cmd := exec.CommandContext(ctx, "docker", "exec", chartContainer,
		"curl", "-sS", "--max-time", "10", fmt.Sprintf("http://127.0.0.1:%d/health", chartPort))
	out, err := cmd.Output()
	if err != nil {
		fmt.Printf("등록된 소스: (조회 실패 — %s 컨테이너가 떠 있는지 확인하세요)\n", chartContainer)
		return
	}
	var h struct {
		Sources []string `json:"sources"`
	}
	if json.Unmarshal(out, &h) == nil && len(h.Sources) > 0 {
		fmt.Printf("등록된 소스: %s\n", strings.Join(h.Sources, ", "))
	}
}
