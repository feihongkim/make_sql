package srv

import "testing"

// --readonly 는 "이 쿼리는 읽기만 한다" 를 보장하려고 붙이는 플래그다.
// 여기서 지키는 것은 두 가지다.
//
//  1. 첫 키워드가 쓰기/변경이면 막는다 (원래 있던 검사)
//  2. 세미콜론으로 문장을 이어붙이면 막는다
//
// 2번이 필요한 이유: dangerousSQL 은 (?m) 없는 ^ 라서 쿼리 맨 앞만 본다.
// "SELECT 1; DROP TABLE x" 는 SELECT 로 판정돼 통과하는데, MSSQL 은 한 요청의
// 문장을 전부 실행하므로 DROP 이 그대로 나간다.
func TestValidateReadOnlyAllowsReadQueries(t *testing.T) {
	for _, q := range []string{
		"SELECT TOP 10 * FROM STOCK_CODE",
		"  select 1  ",
		"WITH c AS (SELECT 1) SELECT * FROM c",
		"SELECT 1;",        // 끝에 붙은 세미콜론은 문장 구분이 아니다
		"SELECT 1;  \n\t;", // 뒤쪽 공백·중복 세미콜론도 마찬가지
	} {
		if err := validateReadOnly(q); err != nil {
			t.Errorf("조회 쿼리가 막혔다: %q → %v", q, err)
		}
	}
}

func TestValidateReadOnlyBlocksWriteKeywords(t *testing.T) {
	for _, q := range []string{
		"DELETE FROM STOCK_CODE",
		"  drop table STOCK_CODE",
		"UPDATE STOCK_CODE SET NAME='x'",
		"exec sp_who",
		"MERGE INTO t USING s ON 1=1",
	} {
		if err := validateReadOnly(q); err == nil {
			t.Errorf("쓰기 쿼리가 통과했다: %q", q)
		}
	}
}

// 위험 키워드가 맨 앞이 아니어도 잡아야 한다. 앵커(^)를 쓰면 아래가 전부
// 통과하는데, T-SQL 은 세미콜론 없이 공백만으로도 문장이 이어지므로
// 뒤에 붙은 문장이 그대로 실행된다.
func TestValidateReadOnlyBlocksKeywordsAnywhere(t *testing.T) {
	for _, q := range []string{
		"/*x*/ DELETE FROM STOCK_CODE",             // 주석으로 시작
		"(DELETE FROM STOCK_CODE)",                 // 괄호로 시작
		"WITH c AS (SELECT 1) DELETE FROM t",       // CTE 로 시작
		"SELECT 1\nUPDATE STOCK_CODE SET NAME='x'", // 줄바꿈으로 이은 두 번째 문장
		"SELECT 1 TRUNCATE TABLE STOCK_CODE",       // 공백으로 이은 두 번째 문장
	} {
		if err := validateReadOnly(q); err == nil {
			t.Errorf("맨 앞이 아닌 위험 키워드가 통과했다: %q", q)
		}
	}
}

// 과다 차단은 의도된 대가다. 안전장치는 막는 쪽으로 기울이고, 막히면
// --readonly 를 빼서 실행한다. 이게 바뀌면 의식적인 결정이어야 한다.
func TestValidateReadOnlyOverblocksKeywordsInLiterals(t *testing.T) {
	for _, q := range []string{
		"SELECT * FROM t WHERE NAME='DELETE'",
		"SELECT [UPDATE] FROM t",
	} {
		if err := validateReadOnly(q); err == nil {
			t.Errorf("과다 차단이 사라졌다 (의도된 동작이 바뀜): %q", q)
		}
	}
}

// 반대로 식별자 안에 든 키워드는 막으면 안 된다. UPDATE_DATE 처럼 밑줄로
// 이어진 컬럼명은 \b 경계에 걸리지 않아 통과해야 한다.
func TestValidateReadOnlyAllowsKeywordsInsideIdentifiers(t *testing.T) {
	for _, q := range []string{
		"SELECT UPDATE_DATE FROM STOCK_CODE",
		"SELECT CREATED_AT, DELETED_YN FROM t",
	} {
		if err := validateReadOnly(q); err != nil {
			t.Errorf("식별자 안의 키워드가 막혔다: %q → %v", q, err)
		}
	}
}

// 이 테스트가 깨지면 --readonly 로 임의의 쓰기가 가능해진다.
func TestValidateReadOnlyBlocksMultipleStatements(t *testing.T) {
	for _, q := range []string{
		"SELECT 1; DROP TABLE STOCK_CODE",
		"SELECT 1;DELETE FROM STOCK_CODE",
		"select * from t ; update t set a=1 ;",
		"SELECT 1;\nTRUNCATE TABLE STOCK_CODE",
	} {
		if err := validateReadOnly(q); err == nil {
			t.Errorf("다중 문장이 통과했다: %q", q)
		}
	}
}
