package srv

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ServerTarget struct {
	Name    string
	Host    string
	Port    string
	User    string
	SSHKey  string
	IsSelf  bool
}

func securityTargets() []ServerTarget {
	return []ServerTarget{
		{Name: "white", Host: "192.168.3.120", Port: "22", User: "feihong", IsSelf: true},
		{Name: "117", Host: "192.168.3.117", Port: "22", User: "feihong"},
		{Name: "130", Host: "192.168.3.130", Port: "22", User: "feihong"},
		{Name: "232", Host: "192.168.3.232", Port: "2222", User: "alvinii"},
	}
}

// sshRun 은 원격 서버에서 명령을 실행하고 출력을 반환한다.
func sshRun(t ServerTarget, cmd string) string {
	args := []string{
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		"-p", t.Port,
	}
	if t.SSHKey != "" {
		args = append(args, "-i", t.SSHKey)
	}
	args = append(args, fmt.Sprintf("%s@%s", t.User, t.Host), cmd)

	out, err := exec.Command("ssh", args...).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("[접속 실패: %v]", err)
	}
	return strings.TrimSpace(string(out))
}

// localRun 은 로컬에서 명령을 실행하고 출력을 반환한다 (에러 무시).
func localRun(cmd string) string {
	out, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
	return strings.TrimSpace(string(out))
}

func runOnTarget(t ServerTarget, cmd string) string {
	if t.IsSelf {
		return localRun(cmd)
	}
	return sshRun(t, cmd)
}

// HandleSecurityCheck 는 모든 서버 보안 점검을 실행하고 요약 문자열을 반환한다.
func HandleSecurityCheck() string {
	targets := securityTargets()
	var report strings.Builder
	kst := time.Now().UTC().Add(9 * time.Hour)
	report.WriteString(fmt.Sprintf("서버 보안 점검 결과 (%s KST)\n\n", kst.Format("2006-01-02 15:04")))

	for _, t := range targets {
		report.WriteString(fmt.Sprintf("=== %s (%s) ===\n", t.Name, t.Host))

		// [1] 외부 연결 (내부망·루프백 제외)
		extConn := runOnTarget(t, "ss -tunp 2>/dev/null | grep ESTAB | grep -v -E '(127\\.0\\.0\\.1|::1|192\\.168\\.|172\\.(1[6-9]|2[0-9]|3[01])\\.)' | grep -v '100\\.' || true")
		if extConn == "" {
			extConn = "없음"
		}
		report.WriteString(fmt.Sprintf("[외부연결] %s\n", extConn))

		// [2] CPU 90% 이상 프로세스 (ps 자신 제외)
		highCPU := runOnTarget(t, "ps aux --sort=-%cpu 2>/dev/null | awk 'NR>1 && $3+0 > 90 && $11 != \"ps\" {print $1,$2,$3\"%\",$11}' | head -5")
		if highCPU == "" {
			highCPU = "없음"
		}
		report.WriteString(fmt.Sprintf("[고CPU] %s\n", highCPU))

		// [3] 최근 실패한 로그인
		failedLogin := runOnTarget(t, "sudo lastb 2>/dev/null | head -5 || lastb 2>/dev/null | head -5")
		if failedLogin == "" {
			failedLogin = "없음"
		}
		report.WriteString(fmt.Sprintf("[로그인실패] %s\n", failedLogin))

		// [4] /tmp 실행 가능 파일 (node_modules 제외)
		execFiles := runOnTarget(t, `find /tmp /var/tmp -type f -perm /111 2>/dev/null | grep -v -E '(node_modules|xfs-|yarn|npm|\.bun)' | head -10`)
		if execFiles == "" {
			execFiles = "없음"
		}
		report.WriteString(fmt.Sprintf("[실행파일] %s\n", execFiles))

		// [5] 크론잡 (모든 유저 + /etc/cron.d)
		cronJobs := runOnTarget(t, `{ for u in $(cut -f1 -d: /etc/passwd); do crontab -u "$u" -l 2>/dev/null | grep -v '^#' | grep -v '^$' | sed "s/^/$u: /"; done; grep -rh -v '^#' /etc/cron.d/ /etc/crontab 2>/dev/null | grep -v '^$'; } | head -20 || true`)
		if cronJobs == "" {
			cronJobs = "없음"
		}
		report.WriteString(fmt.Sprintf("[크론잡] %s\n", cronJobs))

		// [6] 비정상 SUID 파일 (시스템 기본 제외)
		suidFiles := runOnTarget(t, `find / -xdev -perm -4000 -type f 2>/dev/null | grep -v -E '^/(usr/bin|usr/sbin|bin|sbin|usr/lib|usr/libexec|snap)/' | head -10 || true`)
		if suidFiles == "" {
			suidFiles = "없음"
		}
		report.WriteString(fmt.Sprintf("[SUID] %s\n", suidFiles))

		// [7] 비정상 listening 포트 (내부망 외)
		listenPorts := runOnTarget(t, `ss -tlnp 2>/dev/null | grep LISTEN | grep -v -E '(127\.0\.0\.1|::1|\*:22|0\.0\.0\.0:22|\[::]:22)' | awk '{print $4,$6}' | head -15 || true`)
		if listenPorts == "" {
			listenPorts = "없음"
		}
		report.WriteString(fmt.Sprintf("[포트] %s\n", listenPorts))

		report.WriteString("\n")
	}

	return report.String()
}

// (Scheduler) runSecurityCheck 는 점검 결과를 텔레그램으로 전송한다.
func (s *Scheduler) runSecurityCheck() {
	output := HandleSecurityCheck()
	if output == "" {
		fmt.Println("[scheduler] security-check 결과 없음")
		return
	}
	if err := sendTelegramMsg(output); err != nil {
		fmt.Printf("[scheduler] 텔레그램 전송 실패: %v\n", err)
	}
}

