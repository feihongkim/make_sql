package srv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ClaudeProjectConfig 는 claude_project.yaml 구조
type ClaudeProjectConfig struct {
	Projects []struct {
		Name string `yaml:"name"`
		Path string `yaml:"path"`
	} `yaml:"projects"`
}

// loadClaudeProjects 는 claude_project.yaml을 로드합니다
func loadClaudeProjects() (*ClaudeProjectConfig, error) {
	// 실행 파일 기준으로 claude_project.yaml 찾기
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("실행 경로 확인 실패: %w", err)
	}
	configPath := filepath.Join(filepath.Dir(exePath), "claude_project.yaml")

	// 현재 디렉토리에서도 시도
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = "claude_project.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("claude_project.yaml 읽기 실패: %w", err)
	}

	var cfg ClaudeProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("claude_project.yaml 파싱 실패: %w", err)
	}
	return &cfg, nil
}

// HandleClaude 는 claude 서브커맨드를 처리합니다
// ./abledb claude [프로젝트명] [프롬프트]
// ./abledb claude [프로젝트명] @파일명
func HandleClaude(ctx context.Context, args []string) {
	if len(args) < 2 {
		printClaudeUsage()
		return
	}

	projectName := args[0]
	promptArgs := args[1:]

	// 프로젝트 설정 로드
	cfg, err := loadClaudeProjects()
	if err != nil {
		fmt.Printf("설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	// 프로젝트 경로 찾기
	var projectDir string
	for _, p := range cfg.Projects {
		if strings.EqualFold(p.Name, projectName) {
			projectDir = p.Path
			break
		}
	}
	if projectDir == "" {
		fmt.Printf("프로젝트 '%s'를 찾을 수 없습니다.\n", projectName)
		fmt.Println("등록된 프로젝트:")
		for _, p := range cfg.Projects {
			fmt.Printf("  - %s → %s\n", p.Name, p.Path)
		}
		os.Exit(1)
	}

	// 경로 존재 확인
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		fmt.Printf("프로젝트 경로가 존재하지 않습니다: %s\n", projectDir)
		os.Exit(1)
	}

	// 프롬프트 조합 (@파일 지원)
	prompt, err := buildPrompt(promptArgs)
	if err != nil {
		fmt.Printf("프롬프트 처리 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[claude] 프로젝트: %s (%s)\n", projectName, projectDir)
	fmt.Printf("[claude] 프롬프트: %s\n", truncate(prompt, 100))
	fmt.Println("[claude] 실행 중...")

	// claude 실행
	cmd := exec.CommandContext(ctx,
		"claude",
		"-p", prompt,
		"--dangerously-skip-permissions",
	)
	cmd.Dir = projectDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[claude] 실행 오류: %v\n", err)
		if len(out) > 0 {
			fmt.Println(string(out))
		}
		os.Exit(1)
	}

	result := string(out)
	fmt.Println(result)
}

// buildPrompt 는 인자들을 조합하여 프롬프트를 생성합니다
// @파일명이면 파일 내용을 읽어서 사용
func buildPrompt(args []string) (string, error) {
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		filePath := args[0][1:]
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("파일 읽기 실패 '%s': %w", filePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return strings.Join(args, " "), nil
}

// truncate 는 문자열을 최대 길이로 자릅니다
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func printClaudeUsage() {
	fmt.Println("사용법:")
	fmt.Println("  ./abledb claude [프로젝트명] [프롬프트]")
	fmt.Println("  ./abledb claude [프로젝트명] @파일명")
	fmt.Println()
	fmt.Println("예시:")
	fmt.Println("  ./abledb claude MC \"README.md를 업데이트해줘\"")
	fmt.Println("  ./abledb claude KIS \"main.go의 에러 핸들링을 개선해줘\"")
	fmt.Println("  ./abledb claude MC @prompt.txt")
	fmt.Println()

	cfg, err := loadClaudeProjects()
	if err != nil {
		fmt.Printf("(프로젝트 목록 로드 실패: %v)\n", err)
		return
	}
	fmt.Println("등록된 프로젝트:")
	for _, p := range cfg.Projects {
		fmt.Printf("  - %s → %s\n", p.Name, p.Path)
	}
}
