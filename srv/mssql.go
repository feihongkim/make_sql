package srv

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"MakeSQL/console"
)

// dangerousSQL 은 데이터 변경/삭제/구조 변경이 가능한 SQL 키워드를 감지합니다
var dangerousSQL = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|EXEC|EXECUTE|MERGE|GRANT|REVOKE|DENY)\b`)

// validateReadOnly 는 쿼리가 SELECT/WITH 등 읽기 전용인지 검증합니다
func validateReadOnly(query string) error {
	trimmed := strings.TrimSpace(query)
	if dangerousSQL.MatchString(trimmed) {
		return fmt.Errorf("읽기 전용 모드: 쓰기/변경 쿼리가 차단되었습니다 (--readonly)")
	}
	return nil
}

// HandleMSSQL 은 mssql 서브커맨드를 처리합니다
// ./abledb mssql [서버이름] --dblist
// ./abledb mssql [--readonly] [서버이름] [DB이름] [SQL쿼리 또는 @파일명]
func HandleMSSQL(ctx context.Context, args []string) {
	readonly := false
	if len(args) > 0 && args[0] == "--readonly" {
		readonly = true
		args = args[1:]
	}

	if len(args) < 2 {
		fmt.Println("사용법:")
		fmt.Println("  ./abledb mssql [서버이름] --dblist")
		fmt.Println("  ./abledb mssql [--readonly] [서버이름] [DB이름] [SQL쿼리]")
		fmt.Println("  ./abledb mssql [--readonly] [서버이름] [DB이름] @파일명    (sql/ 폴더의 .sql 파일)")
		os.Exit(1)
	}

	serverName := args[0]

	if args[1] == "--dblist" {
		if err := console.PrintMSSQLInfo(serverName); err != nil {
			log.Fatalf("[%s] %v", serverName, err)
		}
		return
	}

	if len(args) < 3 {
		fmt.Println("사용법: ./abledb mssql [--readonly] [서버이름] [DB이름] [SQL쿼리 또는 @파일명]")
		os.Exit(1)
	}
	dbName := args[1]
	query := resolveSQL(args[2])

	if readonly {
		if err := validateReadOnly(query); err != nil {
			log.Fatalf("[%s/%s] %v", serverName, dbName, err)
		}
	}

	result, err := console.RunMSSQLQuery(serverName, dbName, query)
	if err != nil {
		log.Fatalf("[%s/%s] %v", serverName, dbName, err)
	}
	fmt.Println(result)
}

// resolveSQL 은 @파일명이면 sql/ 폴더에서 파일을 읽고, 아니면 그대로 반환합니다
func resolveSQL(input string) string {
	if !strings.HasPrefix(input, "@") {
		return input
	}

	filename := strings.TrimPrefix(input, "@")
	// .sql 확장자 자동 추가
	if !strings.HasSuffix(filename, ".sql") {
		filename += ".sql"
	}

	// sql/ 폴더에서 찾기
	data, err := os.ReadFile("sql/" + filename)
	if err != nil {
		log.Fatalf("SQL 파일 읽기 실패: sql/%s: %v", filename, err)
	}
	return string(data)
}
