package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"MakeSQL/console"
	"MakeSQL/scheduler"
	"MakeSQL/srv"

	"gopkg.in/yaml.v3"
)

func loadConfig(path string) (*console.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config 읽기 실패: %w", err)
	}
	var cfg console.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config 파싱 실패: %w", err)
	}
	return &cfg, nil
}

const (
	defaultTimeout     = 5 * time.Minute
	longRunningTimeout = 30 * time.Minute
)

// longRunningCommands 는 30분 타임아웃을 받는 서브커맨드입니다.
//
// 여기 없는 명령은 defaultTimeout 이 적용됩니다. 오래 걸리는 명령을 추가하고
// 이 표에 넣지 않으면 5분에 조용히 잘리므로, 새 서브커맨드를 만들 때 함께
// 확인해야 합니다. (|| 로 이어 붙인 조건문보다 빠진 것을 눈으로 찾기 쉽습니다.)
var longRunningCommands = map[string]bool{
	"copy":         true,
	"log-analyze":  true,
	"claude":       true,
	"ask":          true,
	"chart":        true,
	"surge-report": true,
	"surge-sync":   true,
	"code-backup":  true,
}

func main() {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	timeout := defaultTimeout
	if len(os.Args) > 1 && longRunningCommands[os.Args[1]] {
		timeout = longRunningTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if len(os.Args) < 2 {
		srv.HandleInfo(ctx, cfg)
		return
	}

	// 서브커맨드 이후의 인자를 전달
	subArgs := os.Args[2:]

	switch os.Args[1] {
	case "mssql":
		srv.HandleMSSQL(ctx, subArgs)
	case "mongo":
		srv.HandleMongo(ctx, cfg, subArgs)
	case "copy":
		srv.HandleCopy(ctx, subArgs)
	case "log-analyze":
		srv.HandleLogAnalyze(ctx, cfg, subArgs)
	case "claude":
		srv.HandleClaude(ctx, subArgs)
	case "ask":
		srv.HandleAsk(ctx, subArgs)
	case "chart":
		srv.HandleChart(ctx, subArgs)
	case "surge-report":
		srv.HandleSurgeReport(ctx, subArgs)
	case "surge-sync":
		srv.RunSurgeSync()
	case "temp-check":
		srv.RunTempCheck()
	case "security-check":
		fmt.Println(srv.HandleSecurityCheck())
	case "code-backup":
		srv.RunCodeBackup()
	case "sonar":
		if len(subArgs) == 0 {
			fmt.Println("사용법: ./abledb sonar [--pro|--reasoning] <검색 질문>")
			os.Exit(1)
		}
		model := "sonar"
		if subArgs[0] == "--pro" {
			model = "sonar-pro"
			subArgs = subArgs[1:]
		} else if subArgs[0] == "--reasoning" {
			model = "sonar-reasoning-pro"
			subArgs = subArgs[1:]
		}
		if len(subArgs) == 0 {
			fmt.Println("검색 질문을 입력하세요")
			os.Exit(1)
		}
		query := strings.Join(subArgs, " ")
		result, err := srv.HandleSonarSearch(model, query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Sonar 검색 실패: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(result)
	case "queue-to-mongo":
		cancel()
		srv.HandleQueueToMongo(subArgs)
		return
	case "scheduler":
		cancel() // 스케줄러는 자체 루프로 동작하므로 타임아웃 해제
		scheduler.HandleScheduler(subArgs)
		return
	default:
		fmt.Printf("알 수 없는 명령: %s\n", os.Args[1])
		fmt.Println("사용법:")
		fmt.Println("  ./abledb                                     모든 MongoDB 정보 출력")
		fmt.Println("  ./abledb mssql [서버] --dblist                MSSQL DB 목록")
		fmt.Println("  ./abledb mssql [--readonly] [서버] [DB] [쿼리|@파일]  MSSQL 쿼리 실행")
		fmt.Println("  ./abledb mongo [연결] --dblist                MongoDB DB 목록")
		fmt.Println("  ./abledb mongo [연결] --drop-before [날짜]    날짜 이전 컬렉션 삭제")
		fmt.Println("  ./abledb mongo [연결] [DB] [JSON|@파일]       MongoDB 명령 실행")
		fmt.Println("  ./abledb log-analyze [연결] [시간(h)]         MongoDB LOG 분석")
		fmt.Println("  ./abledb claude [프로젝트명] [프롬프트|@파일]   Claude 코드 수정")
		fmt.Println("  ./abledb ask [컨테이너명] [프롬프트|@파일] [--new] [--file 경로]  컨테이너 에이전트에 요청/응답")
		fmt.Println("  ./abledb chart [소스] [종목] [--from] [--to] [--boxes] [--telegram]  DB 데이터로 캔들차트 생성")
		fmt.Println("  ./abledb surge-report [YYYYMMDD[-YYYYMMDD]] [--out /path] [--server name]  급등 종목 분석 MD 생성")
		fmt.Println("  ./abledb security-check                      서버 보안 점검 (3대)")
		fmt.Println("  ./abledb scheduler [status|stop]             스케줄러 실행/관리")
		fmt.Println("  ./abledb copy [소스] [소스DB] [대상] [대상DB] [테이블] [조건]  데이터 복사")
		os.Exit(1)
	}
}
