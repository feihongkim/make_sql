package srv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAskArgsDefaults(t *testing.T) {
	opts, err := parseAskArgs([]string{"apply_claude", "README", "요약해줘"})
	if err != nil {
		t.Fatalf("파싱 실패: %v", err)
	}
	if opts.container != "apply_claude" {
		t.Errorf("컨테이너명 = %q", opts.container)
	}
	if opts.port != agentAPIDefaultPort || opts.timeoutSec != agentAskDefaultTimeout {
		t.Errorf("기본 포트/타임아웃이 다르다: %+v", opts)
	}
	if opts.oneshot || opts.sshHost != "" || len(opts.files) != 0 {
		t.Errorf("옵션 기본값이 켜져 있다: %+v", opts)
	}
	// 플래그가 아닌 인자는 프롬프트로 모인다.
	if got := strings.Join(opts.promptArgs, " "); got != "README 요약해줘" {
		t.Errorf("프롬프트 = %q", got)
	}
}

func TestParseAskArgsFlags(t *testing.T) {
	opts, err := parseAskArgs([]string{
		"tem_lms", "작업해줘", "--new", "--host", "alvinii",
		"--timeout", "1800", "--port", "9000",
	})
	if err != nil {
		t.Fatalf("파싱 실패: %v", err)
	}
	if !opts.oneshot {
		t.Error("--new 가 안 먹었다")
	}
	if opts.sshHost != "alvinii" || opts.timeoutSec != 1800 || opts.port != 9000 {
		t.Errorf("옵션이 잘못 파싱됐다: %+v", opts)
	}
	if got := strings.Join(opts.promptArgs, " "); got != "작업해줘" {
		t.Errorf("프롬프트에 플래그가 섞였다: %q", got)
	}
}

// --file 은 파싱 시점에 존재를 확인한다. 컨테이너까지 올라간 뒤 실패하면
// 원격 임시 디렉토리만 남기고 끝난다.
func TestParseAskArgsChecksFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	opts, err := parseAskArgs([]string{"c", "요약", "--file", path})
	if err != nil {
		t.Fatalf("있는 파일이 거부됐다: %v", err)
	}
	if len(opts.files) != 1 || opts.files[0] != path {
		t.Errorf("파일 목록 = %v", opts.files)
	}

	if _, err := parseAskArgs([]string{"c", "요약", "--file", path + ".없음"}); err == nil {
		t.Error("없는 파일이 통과했다")
	}
}

func TestParseAskArgsRejectsBadInput(t *testing.T) {
	cases := []struct {
		desc string
		args []string
	}{
		{"프롬프트 없음", []string{"apply_claude", "--new"}},
		{"타임아웃 값 없음", []string{"c", "p", "--timeout"}},
		{"타임아웃이 숫자가 아님", []string{"c", "p", "--timeout", "곧"}},
		{"타임아웃이 0", []string{"c", "p", "--timeout", "0"}},
		{"포트가 음수", []string{"c", "p", "--port", "-1"}},
		{"host 값 없음", []string{"c", "p", "--host"}},
		{"file 값 없음", []string{"c", "p", "--file"}},
	}
	for _, c := range cases {
		if _, err := parseAskArgs(c.args); err == nil {
			t.Errorf("%s: 통과했다 (%v)", c.desc, c.args)
		}
	}
}

// 업로드한 파일은 프롬프트에 인라인하지 않고 경로만 알려준다.
func TestWithFileReferencesListsPathsNotContent(t *testing.T) {
	got := withFileReferences("이상 징후만 뽑아줘", []string{"/home/me/report.md"}, "/tmp/abledb-ask-1")
	for _, want := range []string{"/tmp/abledb-ask-1/report.md", "/home/me/report.md", "이상 징후만 뽑아줘"} {
		if !strings.Contains(got, want) {
			t.Errorf("출력에 %q 가 없다:\n%s", want, got)
		}
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	// SSH 경로에서 원격 쉘이 문자열을 다시 파싱하므로 따옴표가 새면 명령이 쪼개진다.
	if got, want := shellQuote("it's"), `'it'\''s'`; got != want {
		t.Errorf("shellQuote = %s, want %s", got, want)
	}
}
