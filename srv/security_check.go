package srv

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ServerTarget struct {
	Name   string
	Host   string
	Port   string
	User   string
	SSHKey string
	IsSelf bool
}

func securityTargets() []ServerTarget {
	return []ServerTarget{
		{Name: "white", Host: "192.168.3.120", Port: "22", User: "feihong", IsSelf: true},
		{Name: "117", Host: "192.168.3.117", Port: "22", User: "feihong"},
		{Name: "130", Host: "192.168.3.130", Port: "22", User: "feihong"},
		{Name: "232", Host: "192.168.3.232", Port: "2222", User: "alvinii"},
	}
}

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

		extConn := runOnTarget(t, "ss -tunp 2>/dev/null | grep ESTAB | grep -v -E '(127\\.0\\.0\\.1|::1|192\\.168\\.|172\\.(1[6-9]|2[0-9]|3[01])\\.)' | grep -v '100\\.' || true")
		if extConn == "" {
			extConn = "없음"
		}
		report.WriteString(fmt.Sprintf("[외부연결] %s\n", extConn))

		highCPU := runOnTarget(t, "ps aux --sort=-%cpu 2>/dev/null | awk 'NR>1 && $3+0 > 90 && $11 != \"ps\" {print $1,$2,$3\"%\",$11}' | head -5")
		if highCPU == "" {
			highCPU = "없음"
		}
		report.WriteString(fmt.Sprintf("[고CPU] %s\n", highCPU))

		failedLogin := runOnTarget(t, "sudo lastb 2>/dev/null | head -5 || lastb 2>/dev/null | head -5")
		if failedLogin == "" {
			failedLogin = "없음"
		}
		report.WriteString(fmt.Sprintf("[로그인실패] %s\n", failedLogin))

		execFiles := runOnTarget(t, `find /tmp /var/tmp -type f -perm /111 2>/dev/null | grep -v -E '(node_modules|xfs-|yarn|npm|\.bun)' | head -10`)
		if execFiles == "" {
			execFiles = "없음"
		}
		report.WriteString(fmt.Sprintf("[실행파일] %s\n", execFiles))

		cronJobs := runOnTarget(t, `{ for u in $(cut -f1 -d: /etc/passwd); do crontab -u "$u" -l 2>/dev/null | grep -v '^#' | grep -v '^$' | sed "s/^/$u: /"; done; grep -rh -v '^#' /etc/cron.d/ /etc/crontab 2>/dev/null | grep -v '^$'; } | head -20 || true`)
		if cronJobs == "" {
			cronJobs = "없음"
		}
		report.WriteString(fmt.Sprintf("[크론잡] %s\n", cronJobs))

		suidFiles := runOnTarget(t, `find / -xdev -perm -4000 -type f 2>/dev/null | grep -v -E '^/(usr/bin|usr/sbin|bin|sbin|usr/lib|usr/libexec|snap)/' | head -10 || true`)
		if suidFiles == "" {
			suidFiles = "없음"
		}
		report.WriteString(fmt.Sprintf("[SUID] %s\n", suidFiles))

		listenPorts := runOnTarget(t, `ss -tlnp 2>/dev/null | grep LISTEN | grep -v -E '(127\.0\.0\.1|::1|\*:22|0\.0\.0\.0:22|\[::]:22)' | awk '{print $4,$6}' | head -15 || true`)
		if listenPorts == "" {
			listenPorts = "없음"
		}
		report.WriteString(fmt.Sprintf("[포트] %s\n", listenPorts))

		report.WriteString("\n")
	}

	return report.String()
}
