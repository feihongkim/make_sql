package scheduler

import (
	"strings"
	"testing"
)

// LS 프로젝트가 2026-07-30 부터 공용 LOG 큐에 이벤트를 발행하기 시작했다.
// 완료 요약이 KIS 전용으로 하드코딩돼 있어 LS 가 누락됐던 것을 고쳤다.
//
// 이 테스트가 지키는 것: KIS 와 LS 는 메시지 형식이 달라 같은 정규식으로 파싱되지
// 않는다. KIS 는 태스크명이 [BD.DailyPrice] 처럼 대괄호로 오고, LS 는
// "job 완료: 내용" 형태다. 한쪽 형식만 처리하면 다른 쪽이 조용히 사라진다.
func TestFormatLogAnalyzeMsgSplitsCompletionsByProject(t *testing.T) {
	jsonStr := `{
      "range": {"from":"2026-07-30 09:00","to":"2026-07-30 12:00","hours":3},
      "summary": {"total":100,"errors":0,"api_failure_rate":"0%"},
      "completions": [
        "20260730_101500 [KIS][pipeline] [INFO] [BD.DailyPrice] 완료: 성공=120, 실패=0, 스킵=3, MERGE=4500건",
        "20260730_102000 [LS][scheduler] [INFO] investor-trading 완료: OK t1601 (3.4s)",
        "20260730_110000 [LS][scheduler] [INFO] credit-trading 완료: 359청크 4298건 (227분)"
      ]
    }`

	got := formatLogAnalyzeMsg(jsonStr)

	for _, want := range []string{
		"【KIS 파이프라인 완료 (1개 태스크)】",
		"BD.DailyPrice",
		"【LS 수집 완료 (2개 job)】",
		"investor-trading",
		"credit-trading",
		"359청크 4298건 (227분)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("출력에 %q 가 없다\n--- 실제 출력 ---\n%s", want, got)
		}
	}
}

// LS 완료가 하나도 없으면 LS 섹션 자체가 나오지 않아야 한다.
// (기존 KIS 전용 리포트 모양이 유지되는지 확인)
func TestFormatLogAnalyzeMsgOmitsEmptyLSSection(t *testing.T) {
	jsonStr := `{
      "range": {"from":"2026-07-30 09:00","to":"2026-07-30 12:00","hours":3},
      "summary": {"total":10,"errors":0,"api_failure_rate":"0%"},
      "completions": [
        "20260730_101500 [KIS][pipeline] [INFO] [BD.DailyPrice] 완료: 성공=1, 실패=0, 스킵=0, MERGE=1건"
      ]
    }`

	got := formatLogAnalyzeMsg(jsonStr)
	if strings.Contains(got, "LS 수집 완료") {
		t.Errorf("LS 완료가 없는데 LS 섹션이 나왔다\n%s", got)
	}
	if !strings.Contains(got, "KIS 파이프라인 완료") {
		t.Errorf("KIS 섹션이 사라졌다\n%s", got)
	}
}

// 형식이 어긋난 완료 메시지는 건너뛰되, 다른 항목 표시를 막지 않아야 한다.
func TestFormatLogAnalyzeMsgSkipsMalformedCompletions(t *testing.T) {
	jsonStr := `{
      "range": {"from":"2026-07-30 09:00","to":"2026-07-30 12:00","hours":3},
      "summary": {"total":10,"errors":0,"api_failure_rate":"0%"},
      "completions": [
        "20260730_101500 [LS][scheduler] 완료:",
        "20260730_102000 [LS][scheduler] [INFO] investor-trading 완료: OK t1601 (3.4s)"
      ]
    }`

	got := formatLogAnalyzeMsg(jsonStr)
	if !strings.Contains(got, "investor-trading") {
		t.Errorf("정상 항목이 표시되지 않았다\n%s", got)
	}
}
