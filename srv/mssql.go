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

// dangerousSQL 은 데이터 변경/삭제/구조 변경이 가능한 SQL 키워드를 감지합니다.
//
// 앵커(^)를 쓰지 않고 쿼리 **어디에 있든** 잡습니다. 맨 앞 키워드만 보면 아래가
// 모두 통과합니다 — T-SQL 은 세미콜론 없이 공백만으로도 문장이 이어집니다.
//
//	SELECT 1
//	UPDATE STOCK_CODE SET NAME='x'     ← 두 번째 문장
//	/*x*/ DELETE FROM STOCK_CODE       ← 주석으로 시작
//	(DELETE FROM STOCK_CODE)           ← 괄호로 시작
//	WITH c AS (SELECT 1) DELETE ...    ← CTE 로 시작
//
// 대가로 문자열 리터럴이나 대괄호 식별자에 키워드가 든 정상 조회도 막힙니다
// (WHERE NAME='DELETE'). 안전장치는 과하게 막는 쪽이 맞고, 막히면 --readonly 를
// 빼면 됩니다.
var dangerousSQL = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|EXEC|EXECUTE|MERGE|GRANT|REVOKE|DENY)\b`)

// hasMultipleStatements 는 세미콜론으로 문장을 이어붙인 쿼리를 잡습니다.
//
// MSSQL 은 한 번의 요청에 담긴 여러 문장을 순서대로 전부 실행합니다. 그런데
// dangerousSQL 은 (?m) 없는 ^ 라서 쿼리 **맨 앞 키워드 하나만** 봅니다.
// 그래서 "SELECT 1; DROP TABLE x" 는 SELECT 로 판정돼 통과하고, 서버는 DROP 까지
// 실행합니다. 문장이 하나인지 먼저 확인해서 그 경로를 막습니다.
//
// 끝에 붙은 세미콜론은 문장 구분이 아니므로 허용합니다 ("SELECT 1;").
// 문자열 리터럴 안의 세미콜론까지 가려내지는 않습니다 — 안전장치는 과하게 막는
// 쪽이 맞고, 막혔을 때는 --readonly 를 빼면 됩니다.
func hasMultipleStatements(query string) bool {
	return strings.Contains(strings.TrimRight(query, "; \t\r\n"), ";")
}

// validateReadOnly 는 쿼리가 SELECT/WITH 등 읽기 전용인지 검증합니다
//
// ⚠️ 문자열 검사인 이상 완전한 방어는 될 수 없습니다. 예를 들어
// "SELECT * INTO 새테이블 FROM t" 는 테이블을 만들지만 위 키워드 목록에 걸리지
// 않습니다. 근본 해법은 조회 전용 DB 계정으로 연결하는 것입니다
// (chart 서비스의 chart_ro 와 같은 방식이며, 권한을 SQL Server 가 강제합니다).
func validateReadOnly(query string) error {
	trimmed := strings.TrimSpace(query)
	if dangerousSQL.MatchString(trimmed) {
		return fmt.Errorf("읽기 전용 모드: 쓰기/변경 쿼리가 차단되었습니다 (--readonly)")
	}
	if hasMultipleStatements(trimmed) {
		return fmt.Errorf("읽기 전용 모드: 세미콜론으로 이어붙인 다중 문장은 차단됩니다 (--readonly). " +
			"첫 문장만 검사되므로 뒤에 붙은 문장이 그대로 실행될 수 있습니다")
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
