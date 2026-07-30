package srv

import (
	"strings"
	"testing"
	"time"
)

func TestParseChartArgsDefaults(t *testing.T) {
	opts, err := parseChartArgs([]string{"kor_daily", "005930"})
	if err != nil {
		t.Fatalf("기본 인자가 거부됐다: %v", err)
	}
	if opts.source != "kor_daily" || opts.symbol != "005930" {
		t.Errorf("소스/종목이 잘못 파싱됐다: %+v", opts)
	}
	// 기간을 안 주면 최근 6개월이다.
	if want := time.Now().Format("20060102"); opts.to != want {
		t.Errorf("--to 기본값 = %s, want %s", opts.to, want)
	}
	if want := time.Now().AddDate(0, -6, 0).Format("20060102"); opts.from != want {
		t.Errorf("--from 기본값 = %s, want %s", opts.from, want)
	}
	if opts.boxes || opts.telegram {
		t.Errorf("플래그 기본값이 켜져 있다: %+v", opts)
	}
}

// box 유/무를 같은 종목·구간으로 뽑을 때 서로 덮어쓰면 안 된다.
func TestParseChartArgsOutputNameSeparatesBoxes(t *testing.T) {
	plain, err := parseChartArgs([]string{"ls_daily", "000020", "--from", "20250101", "--to", "20260730"})
	if err != nil {
		t.Fatalf("파싱 실패: %v", err)
	}
	boxed, err := parseChartArgs([]string{"ls_daily", "000020", "--from", "20250101", "--to", "20260730", "--boxes"})
	if err != nil {
		t.Fatalf("파싱 실패: %v", err)
	}
	if plain.out == boxed.out {
		t.Errorf("box 유/무 파일명이 같다: %s", plain.out)
	}
	if !strings.HasSuffix(boxed.out, "_box.png") {
		t.Errorf("box 파일명에 _box 접미사가 없다: %s", boxed.out)
	}
}

func TestParseChartArgsFlags(t *testing.T) {
	opts, err := parseChartArgs([]string{
		"kis_daily", "005930",
		"--from", "20260101", "--to", "20260720",
		"--out", "/tmp/x.png", "--title", "제목", "--boxes", "--telegram",
	})
	if err != nil {
		t.Fatalf("파싱 실패: %v", err)
	}
	if opts.from != "20260101" || opts.to != "20260720" {
		t.Errorf("기간이 잘못 파싱됐다: %+v", opts)
	}
	if opts.out != "/tmp/x.png" || opts.title != "제목" {
		t.Errorf("out/title 이 잘못 파싱됐다: %+v", opts)
	}
	if !opts.boxes || !opts.telegram {
		t.Errorf("불리언 플래그가 안 켜졌다: %+v", opts)
	}
}

func TestParseChartArgsRejectsBadInput(t *testing.T) {
	cases := []struct {
		desc string
		args []string
	}{
		{"YYYYMMDD 아닌 날짜", []string{"kor_daily", "005930", "--from", "2026-01-01"}},
		{"존재하지 않는 날짜", []string{"kor_daily", "005930", "--to", "20260231"}},
		{"값 없는 옵션", []string{"kor_daily", "005930", "--from"}},
		{"오타 난 옵션", []string{"kor_daily", "005930", "--box"}},
		{"알 수 없는 옵션", []string{"kor_daily", "005930", "--nope", "1"}},
	}
	for _, c := range cases {
		if _, err := parseChartArgs(c.args); err == nil {
			t.Errorf("%s: 통과했다 (%v)", c.desc, c.args)
		}
	}
}

func TestTruncateCountsRunesNotBytes(t *testing.T) {
	// 한글은 1글자가 3바이트다. 바이트로 자르면 글자가 깨진다.
	got := truncate("가나다라마", 3)
	if got != "가나다..." {
		t.Errorf("truncate = %q, want %q", got, "가나다...")
	}
	if got := truncate("가나", 5); got != "가나" {
		t.Errorf("짧은 문자열이 변형됐다: %q", got)
	}
}
