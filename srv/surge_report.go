package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"MakeSQL/console"
)

type surgeStock struct {
	ContentID   string
	Code        string
	Name        string
	Date        string
	SurgeRate   string
	Summary     string
	WatchPoints []string
	Details     []surgeDetail
}

type surgeDetail struct {
	Category string
	Impact   string
	Detail   string
	Sources  []string
}

// HandleSurgeReport 급등 종목 분석 보고서를 생성합니다
// ./abledb surge-report [YYYYMMDD[-YYYYMMDD]] [--out /path] [--server name] [--format mdx] [--all] [--deploy user@host:port:path]
func HandleSurgeReport(ctx context.Context, args []string) {
	startDate, endDate, outDir, server, format, all, deploy := parseSurgeArgs(args)

	if all {
		handleSurgeAll(server, outDir, format, deploy)
		return
	}

	generateAndSave(server, startDate, endDate, outDir, format)

	if deploy != "" {
		deployFiles(outDir, deploy, format)
	}
}

func handleSurgeAll(server, outDir, format, deploy string) {
	rows, err := console.QueryMSSQLRows(server, "News",
		"SELECT DISTINCT CONVERT(VARCHAR(8), stck_bsop_date, 112) AS dt FROM SurgeAnalysis ORDER BY dt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "날짜 목록 조회 실패: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "디렉토리 생성 실패: %v\n", err)
		os.Exit(1)
	}

	for _, row := range rows {
		dt := strings.TrimSpace(row["dt"])
		if dt == "" {
			continue
		}
		generateAndSave(server, dt, dt, outDir, format)
	}

	if deploy != "" {
		deployFiles(outDir, deploy, format)
	}
}

func generateAndSave(server, startDate, endDate, outDir, format string) {
	stocks, err := fetchSurgeStocks(server, startDate, endDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] SurgeAnalysis 조회 실패: %v\n", startDate, err)
		return
	}
	if len(stocks) == 0 {
		return
	}

	details, err := fetchSurgeDetails(server, startDate, endDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] SurgeAnalysisDetail 조회 실패: %v\n", startDate, err)
		return
	}
	for i, s := range stocks {
		stocks[i].Details = details[s.ContentID]
	}

	ext := ".md"
	if format == "mdx" {
		ext = ".mdx"
	}

	// 파일명: YYMMDD (단일) 또는 surge_YYYYMMDD_YYYYMMDD (범위)
	var filename string
	if startDate == endDate {
		filename = startDate[2:] + ext // YYMMDD
	} else {
		filename = fmt.Sprintf("surge_%s_%s%s", startDate, endDate, ext)
	}
	outPath := filepath.Join(outDir, filename)

	var content string
	body := renderSurgeBody(stocks, startDate, endDate)
	if format == "mdx" {
		content = renderMDXFrontmatter(stocks, startDate) + body
	} else {
		content = renderMarkdownHeader(startDate, endDate) + body
	}

	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "파일 저장 실패 %s: %v\n", outPath, err)
		return
	}
	console.Log("저장: %s (%d개 종목)", outPath, len(stocks))
}

func deployFiles(outDir, deploy, format string) {
	// deploy 형식: "user@host:port:remote_path"
	parts := strings.SplitN(deploy, ":", 3)
	if len(parts) != 3 {
		fmt.Fprintf(os.Stderr, "--deploy 형식 오류: user@host:port:remote_path\n")
		return
	}
	userHost := parts[0]
	port := parts[1]
	remotePath := parts[2]

	ext := "*.md"
	if format == "mdx" {
		ext = "*.mdx"
	}

	pattern := filepath.Join(outDir, ext)
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		console.Log("배포할 파일 없음 (%s)", pattern)
		return
	}

	cmdArgs := []string{"-i", "/root/.ssh/id_ed25519", "-P", port}
	cmdArgs = append(cmdArgs, matches...)
	cmdArgs = append(cmdArgs, fmt.Sprintf("%s:%s", userHost, remotePath))

	out, err := exec.Command("scp", cmdArgs...).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "배포 실패: %v\n%s\n", err, string(out))
		return
	}
	console.Log("배포 완료: %d개 파일 → %s@%s:%s", len(matches), userHost, port, remotePath)
}

func parseSurgeArgs(args []string) (startDate, endDate, outDir, server, format string, all bool, deploy string) {
	outDir = "/data3/News"
	server = "white"
	format = "md"
	today := time.Now().Format("20060102")
	startDate = today
	endDate = today

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 < len(args) {
				outDir = args[i+1]
				i++
			}
		case "--server":
			if i+1 < len(args) {
				server = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				format = args[i+1]
				i++
			}
		case "--deploy":
			if i+1 < len(args) {
				deploy = args[i+1]
				i++
			}
		case "--all":
			all = true
		default:
			if isDateArg(args[i]) {
				parts := strings.SplitN(args[i], "-", 2)
				if len(parts) == 2 && len(parts[0]) == 8 {
					startDate = parts[0]
					endDate = parts[1]
				} else {
					startDate = args[i]
					endDate = args[i]
				}
			}
		}
	}
	return
}

func isDateArg(s string) bool {
	if len(s) < 8 {
		return false
	}
	for _, r := range s[:8] {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func fetchSurgeStocks(server, startDate, endDate string) ([]surgeStock, error) {
	query := fmt.Sprintf(`
SELECT content_id, stck_shrn_iscd, stck_name,
       CONVERT(VARCHAR(8), stck_bsop_date, 112) AS stck_bsop_date,
       CAST(surge_rate AS NVARCHAR(50)) AS surge_rate,
       summary, watch_points
FROM SurgeAnalysis
WHERE stck_bsop_date >= '%s' AND stck_bsop_date <= '%s'
ORDER BY stck_bsop_date DESC, CAST(surge_rate AS FLOAT) DESC`, startDate, endDate)

	rows, err := console.QueryMSSQLRows(server, "News", query)
	if err != nil {
		return nil, err
	}

	var stocks []surgeStock
	for _, row := range rows {
		s := surgeStock{
			ContentID: row["content_id"],
			Code:      row["stck_shrn_iscd"],
			Name:      row["stck_name"],
			Date:      formatDate(row["stck_bsop_date"]),
			SurgeRate: row["surge_rate"],
			Summary:   row["summary"],
		}
		parseJSONStringArray(row["watch_points"], &s.WatchPoints)
		stocks = append(stocks, s)
	}
	return stocks, nil
}

func fetchSurgeDetails(server, startDate, endDate string) (map[string][]surgeDetail, error) {
	query := fmt.Sprintf(`
SELECT sd.content_id, sd.category, sd.impact, sd.detail, sd.sources
FROM SurgeAnalysisDetail sd
JOIN SurgeAnalysis sa ON sd.content_id = sa.content_id
WHERE sa.stck_bsop_date >= '%s' AND sa.stck_bsop_date <= '%s'
ORDER BY sd.content_id,
  CASE LOWER(sd.impact) WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END`, startDate, endDate)

	rows, err := console.QueryMSSQLRows(server, "News", query)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]surgeDetail)
	for _, row := range rows {
		d := surgeDetail{
			Category: row["category"],
			Impact:   strings.ToUpper(row["impact"]),
			Detail:   row["detail"],
		}
		parseJSONStringArray(row["sources"], &d.Sources)
		cid := row["content_id"]
		result[cid] = append(result[cid], d)
	}
	return result, nil
}

func renderMDXFrontmatter(stocks []surgeStock, date string) string {
	isoDate := fmt.Sprintf("%s-%s-%s", date[:4], date[4:6], date[6:8])

	var names []string
	for _, s := range stocks {
		names = append(names, fmt.Sprintf("%s +%s%%", s.Name, s.SurgeRate))
	}
	summary := strings.Join(names, ", ")

	// 태그: 기본 + 종목 수에 따른 규모 태그
	tags := `'주식', '급등', '분석'`

	return fmt.Sprintf("---\ntitle: '급등락 종목 분석 %s'\ndate: '%s'\ntags: [%s]\ndraft: false\nsummary: '%s'\n---\n",
		isoDate, isoDate, tags, summary)
}

func renderMarkdownHeader(startDate, endDate string) string {
	dateLabel := formatDate(startDate)
	if startDate != endDate {
		dateLabel = fmt.Sprintf("%s ~ %s", formatDate(startDate), formatDate(endDate))
	}
	return fmt.Sprintf("# 급등락 종목 분석 — %s\n\n> DB: News.dbo.SurgeAnalysis / SurgeAnalysisDetail  \n> 생성일: %s\n\n---\n\n",
		dateLabel, time.Now().Format("2006-01-02"))
}

func renderSurgeBody(stocks []surgeStock, startDate, endDate string) string {
	var b strings.Builder

	// 종목 목록 테이블
	b.WriteString("## 종목 목록\n\n")
	b.WriteString("| 종목코드 | 종목명 | 날짜 | 급등률 |\n")
	b.WriteString("|---------|-------|------|-------|\n")
	for _, s := range stocks {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | +%s%% |\n",
			s.Code, s.Name, s.Date, s.SurgeRate))
	}
	b.WriteString("\n---\n")

	// 종목별 상세
	for i, s := range stocks {
		b.WriteString(fmt.Sprintf("\n## %d. %s (%s) — +%s%%\n\n", i+1, s.Name, s.Code, s.SurgeRate))

		b.WriteString("### 요약\n")
		b.WriteString(s.Summary + "\n")

		if len(s.Details) > 0 {
			b.WriteString("\n### 상세 분석\n")
			for j, d := range s.Details {
				b.WriteString(fmt.Sprintf("\n#### %d. %s `영향도: %s`\n", j+1, d.Category, d.Impact))
				if d.Detail != "" {
					b.WriteString("　" + d.Detail + "\n") // 전각 공백 1개
				}
				if len(d.Sources) > 0 {
					b.WriteString("\n")
					for _, src := range d.Sources {
						if src != "" {
							b.WriteString(fmt.Sprintf("- %s\n", src))
						}
					}
				}
			}
		}

		if len(s.WatchPoints) > 0 {
			b.WriteString("\n### 모니터링 포인트\n")
			for k, wp := range s.WatchPoints {
				b.WriteString(fmt.Sprintf("%d. %s\n", k+1, wp))
			}
		}

		b.WriteString("\n---\n")
	}

	b.WriteString(fmt.Sprintf("\n*생성: ./abledb surge-report, %s*\n", time.Now().Format("2006-01-02")))
	return b.String()
}

// formatDate YYYYMMDD → YYYY.MM.DD
func formatDate(d string) string {
	d = strings.TrimSpace(d)
	if len(d) == 8 {
		return fmt.Sprintf("%s.%s.%s", d[:4], d[4:6], d[6:8])
	}
	return d
}

func parseJSONStringArray(raw string, out *[]string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "NULL" {
		return
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return
	}
	*out = arr
}
