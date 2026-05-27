package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"MakeSQL/console"
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

func main() {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	timeout := 5 * time.Minute
	if len(os.Args) > 1 && (os.Args[1] == "copy" || os.Args[1] == "log-analyze" || os.Args[1] == "claude" || os.Args[1] == "docker-claude" || os.Args[1] == "send") {
		timeout = 30 * time.Minute
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
	case "docker-claude":
		srv.HandleDockerClaude(ctx, subArgs)
	case "send":
		srv.HandleSend(ctx, subArgs)
	case "security-check":
		fmt.Println(srv.HandleSecurityCheck())
	case "scheduler":
		cancel() // 스케줄러는 자체 루프로 동작하므로 타임아웃 해제
		srv.HandleScheduler(subArgs)
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
		fmt.Println("  ./abledb docker-claude [컨테이너명] [프롬프트|@파일] Docker Claude 실행")
		fmt.Println("  ./abledb send [컨테이너명] [프롬프트|@파일]      Docker Claude 세션에 안전하게 메시지 전송")
		fmt.Println("  ./abledb security-check                      서버 보안 점검 (4대)")
	fmt.Println("  ./abledb scheduler [status|stop]             스케줄러 실행/관리")
		fmt.Println("  ./abledb copy [소스] [소스DB] [대상] [대상DB] [테이블] [조건]  데이터 복사")
		os.Exit(1)
	}
}
