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

// agentapi 채널 서버의 기본 포트. 컨테이너 내부 127.0.0.1 에만 바인딩되어 있다.
const agentAPIDefaultPort = 8799

// 세션이 응답할 때까지 기다리는 기본 시간(초).
const agentAskDefaultTimeout = 900

// 일회성 실행(--new)에서 쓸 settings. 텔레그램과 agentapi 를 모두 비활성화한다.
//
// ⚠️ settings-notelegram.json 을 대신 쓰면 안 된다. 그쪽은 agentapi 가 활성이라
// 일회성 프로세스가 채널 포트를 뺏으려다 실패하고, 그 실패가
// ~/.claude/mcp-needs-auth-cache.json 에 기록된다. 이 파일은 호스트 공유 경로라
// 다음 재시작부터 전 컨테이너에서 채널이 죽는다. (런북 10-6)
const agentOneshotSettings = "/home/feihong/.defaults/settings-oneshot.json"

type agentAskResponse struct {
	RequestID string `json:"request_id"`
	Output    string `json:"output"`
	Error     string `json:"error"`
}

type askOptions struct {
	container  string
	sshHost    string
	port       int
	timeoutSec int
	oneshot    bool
	files      []string
	promptArgs []string
}

// HandleAsk 는 ask 서브커맨드를 처리합니다.
//
//	./abledb ask [컨테이너명] [프롬프트|@파일] [옵션]
//
// 기본은 채널 모드입니다. 컨테이너에서 실행 중인 Claude Code 세션에 agentapi 채널로
// 프롬프트를 넣고 응답을 동기로 받습니다. 컨테이너 안에서 claude 를 새로 띄우지 않으므로
// 텔레그램 봇이 죽지 않고, 기존 세션 문맥도 그대로 이어집니다.
//
// --new 를 주면 일회성 모드입니다. 빈 컨텍스트에서 claude -p 를 새로 실행합니다.
func HandleAsk(ctx context.Context, args []string) {
	if len(args) < 2 {
		printAskUsage()
		return
	}

	opts, err := parseAskArgs(args)
	if err != nil {
		console.LogError("%v", err)
		printAskUsage()
		os.Exit(1)
	}

	prompt, err := buildPrompt(opts.promptArgs)
	if err != nil {
		console.LogError("프롬프트 처리 실패: %v", err)
		os.Exit(1)
	}

	// --file 로 넘긴 자료는 프롬프트에 인라인하지 않고 컨테이너에 올린 뒤 경로만 알려준다.
	// 큰 문서를 통째로 컨텍스트에 밀어 넣지 않아도 되고, 에이전트가 필요한 부분만 읽는다.
	if len(opts.files) > 0 {
		remoteDir, err := uploadFiles(ctx, opts)
		if err != nil {
			console.LogError("[ask] 파일 전달 실패: %v", err)
			os.Exit(1)
		}
		defer cleanupRemoteDir(opts, remoteDir)
		prompt = withFileReferences(prompt, opts.files, remoteDir)
	}

	target := opts.container
	if opts.sshHost != "" {
		target = opts.sshHost + ":" + opts.container
	}
	mode := "채널"
	if opts.oneshot {
		mode = "일회성"
	}
	console.Log("[ask] 대상: %s (%s 모드, timeout %ds)", target, mode, opts.timeoutSec)
	console.Log("[ask] 프롬프트: %s", truncate(prompt, 100))

	var output string
	if opts.oneshot {
		output, err = runOneshot(ctx, opts, prompt)
	} else {
		output, err = runChannel(ctx, opts, prompt)
	}
	if err != nil {
		console.LogError("[ask] %v", err)
		os.Exit(1)
	}

	fmt.Println(output)
}

func parseAskArgs(args []string) (askOptions, error) {
	opts := askOptions{
		container:  args[0],
		port:       agentAPIDefaultPort,
		timeoutSec: agentAskDefaultTimeout,
	}

	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--new":
			opts.oneshot = true
		case "--timeout", "--port":
			flag := rest[i]
			if i+1 >= len(rest) {
				return opts, fmt.Errorf("%s 값이 없습니다", flag)
			}
			n, err := strconv.Atoi(rest[i+1])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("%s 값이 올바르지 않습니다: %s", flag, rest[i+1])
			}
			if flag == "--timeout" {
				opts.timeoutSec = n
			} else {
				opts.port = n
			}
			i++
		case "--host":
			if i+1 >= len(rest) {
				return opts, fmt.Errorf("--host 값이 없습니다")
			}
			opts.sshHost = rest[i+1]
			i++
		case "--file":
			if i+1 >= len(rest) {
				return opts, fmt.Errorf("--file 값이 없습니다")
			}
			path := rest[i+1]
			if _, err := os.Stat(path); err != nil {
				return opts, fmt.Errorf("파일을 읽을 수 없습니다 '%s': %w", path, err)
			}
			opts.files = append(opts.files, path)
			i++
		default:
			opts.promptArgs = append(opts.promptArgs, rest[i])
		}
	}

	if len(opts.promptArgs) == 0 {
		return opts, fmt.Errorf("프롬프트가 없습니다")
	}
	return opts, nil
}

// runChannel 은 agentapi 채널로 요청을 보내고 응답을 받습니다.
func runChannel(ctx context.Context, opts askOptions, prompt string) (string, error) {
	// 프롬프트는 stdin 으로 넘긴다. 인자로 넘기면 긴 프롬프트에서 E2BIG 이 나고
	// 쉘 이스케이프도 필요해진다.
	body, err := json.Marshal(map[string]any{
		"prompt":     prompt,
		"timeout_ms": opts.timeoutSec * 1000,
	})
	if err != nil {
		return "", fmt.Errorf("요청 생성 실패: %w", err)
	}

	// curl 자체 타임아웃은 서버 타임아웃보다 넉넉하게 잡는다. 그래야 504 응답을
	// 받아 원인을 알 수 있고, curl 이 먼저 끊어 원인 불명이 되는 일이 없다.
	cmd := buildCmd(ctx, opts.sshHost, []string{
		"docker", "exec", "-i", opts.container,
		"curl", "-sS", "--max-time", strconv.Itoa(opts.timeoutSec + 30),
		"-X", "POST", fmt.Sprintf("http://127.0.0.1:%d/run", opts.port),
		"-H", "Content-Type: application/json",
		"-d", "@-",
	})
	cmd.Stdin = bytes.NewReader(body)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("실행 오류: %v\n%s\n%s", err, strings.TrimSpace(stderr.String()), strings.TrimSpace(string(out)))
	}

	var resp agentAskResponse
	if err := json.Unmarshal(bytes.TrimSpace(out), &resp); err != nil {
		return "", fmt.Errorf("응답 파싱 실패: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	if resp.Error != "" {
		return "", fmt.Errorf("세션 오류: %s", resp.Error)
	}
	return resp.Output, nil
}

// runOneshot 은 컨테이너 안에서 claude -p 를 새로 실행합니다.
// 빈 컨텍스트에서 시작하므로 기존 세션의 대화를 오염시키지 않고, 큐도 타지 않습니다.
func runOneshot(ctx context.Context, opts askOptions, prompt string) (string, error) {
	// -u 를 지정하지 않는다. 이미지의 기본 USER 를 그대로 쓰면 구세대 컨테이너(node)와
	// 런북 컨테이너(feihong) 양쪽에서 동작한다.
	//
	// 프롬프트는 stdin 으로 넘긴다(E2BIG 방지). settings 는 반드시 oneshot 전용을 쓴다.
	cmd := buildCmd(ctx, opts.sshHost, []string{
		"docker", "exec", "-i", "-w", "/workspace", opts.container,
		"claude", "-p", "--dangerously-skip-permissions",
		"--settings", agentOneshotSettings,
	})
	cmd.Stdin = strings.NewReader(prompt)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("일회성 실행 오류: %v\n%s\n%s", err, strings.TrimSpace(stderr.String()), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// uploadFiles 는 --file 로 지정한 자료를 컨테이너의 임시 디렉토리로 올립니다.
// docker cp 대신 stdin 파이프를 쓰므로 SSH 원격에서도 같은 코드가 동작합니다.
func uploadFiles(ctx context.Context, opts askOptions) (string, error) {
	remoteDir := fmt.Sprintf("/tmp/abledb-ask-%d", time.Now().UnixNano())

	for _, path := range opts.files {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("읽기 실패 '%s': %w", path, err)
		}
		dest := remoteDir + "/" + filepath.Base(path)
		script := fmt.Sprintf("mkdir -p %s && cat > %s", shellQuote(remoteDir), shellQuote(dest))

		cmd := buildCmd(ctx, opts.sshHost, []string{
			"docker", "exec", "-i", opts.container, "sh", "-c", script,
		})
		cmd.Stdin = bytes.NewReader(data)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("업로드 실패 '%s': %v\n%s", path, err, strings.TrimSpace(stderr.String()))
		}
		console.Log("[ask] 파일 전달: %s → %s (%d bytes)", path, dest, len(data))
	}
	return remoteDir, nil
}

func cleanupRemoteDir(opts askOptions, remoteDir string) {
	// 요청이 끝난 뒤 임시 파일을 지운다. 실패해도 본 작업에는 영향이 없으므로 경고만 남긴다.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := buildCmd(ctx, opts.sshHost, []string{
		"docker", "exec", opts.container, "rm", "-rf", remoteDir,
	})
	if err := cmd.Run(); err != nil {
		console.LogError("[ask] 임시 디렉토리 정리 실패 (%s): %v", remoteDir, err)
	}
}

// withFileReferences 는 업로드한 파일 경로를 프롬프트 앞에 덧붙입니다.
func withFileReferences(prompt string, files []string, remoteDir string) string {
	var b strings.Builder
	b.WriteString("다음 파일이 이 컨테이너에 준비되어 있다. 필요한 것을 Read 해서 작업해라.\n")
	for _, path := range files {
		fmt.Fprintf(&b, "- %s/%s (원본: %s)\n", remoteDir, filepath.Base(path), path)
	}
	b.WriteString("\n")
	b.WriteString(prompt)
	return b.String()
}

// buildCmd 는 로컬 실행과 SSH 원격 실행을 같은 인자 배열로 처리합니다.
// SSH 는 원격 쉘이 문자열을 다시 파싱하므로 각 인자를 따옴표로 감싸야 합니다.
func buildCmd(ctx context.Context, sshHost string, args []string) *exec.Cmd {
	if sshHost == "" {
		return exec.CommandContext(ctx, args[0], args[1:]...)
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return exec.CommandContext(ctx, "ssh", sshHost, strings.Join(quoted, " "))
}

// shellQuote 는 bash 단일 따옴표로 문자열을 안전하게 감쌉니다.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func printAskUsage() {
	fmt.Println("사용법:")
	fmt.Println("  ./abledb ask [컨테이너명] [프롬프트] [옵션]")
	fmt.Println("  ./abledb ask [컨테이너명] @파일명 [옵션]")
	fmt.Println()
	fmt.Println("옵션:")
	fmt.Println("  --new           일회성 실행. 빈 컨텍스트에서 시작하고 기존 세션을 오염시키지 않음")
	fmt.Println("  --file 경로     자료 파일을 컨테이너에 올리고 경로만 알려줌 (반복 지정 가능)")
	fmt.Println("  --timeout 초    세션 응답 대기 시간 (기본 900)")
	fmt.Println("  --host  이름    원격 호스트의 컨테이너에 SSH 로 접속")
	fmt.Println("  --port  포트    agentapi 포트 (기본 8799)")
	fmt.Println()
	fmt.Println("예시:")
	fmt.Println("  ./abledb ask apply_claude \"README.md 요약해줘\"")
	fmt.Println("  ./abledb ask apply_claude --file report.md \"이상 징후만 뽑아줘\" --new")
	fmt.Println("  ./abledb ask tem_lms @task.txt --host alvinii --timeout 1800")
	fmt.Println()
	fmt.Println("기본(채널) 모드는 실행 중인 세션에 요청을 넣으므로 문맥이 이어집니다.")
	fmt.Println("--new 는 별도 프로세스로 돌려 문맥 없이 처리하고, 큐를 기다리지 않습니다.")
}
