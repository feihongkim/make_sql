package srv

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"MakeSQL/console"
)

const (
	backupRemoteBase = "/mnt/storage/backups"
	backupDestHost   = "100.122.50.38"
	backupDestUser   = "feihong"
)

type backupTarget struct {
	name      string
	host      string
	port      string
	user      string
	srcDir    string
	isLocal   bool
	feisaSelf bool
}

func backupTargets() []backupTarget {
	return []backupTarget{
		{name: "white", host: "", port: "", user: "", srcDir: "/home/feihong/code/", isLocal: true},
		{name: "alvinii", host: "192.168.3.232", port: "2222", user: "alvinii", srcDir: "/home/alvinii/code/"},
		{name: "feisa", host: "100.122.50.38", port: "22", user: "feihong", srcDir: "/home/feihong/Code/", feisaSelf: true},
	}
}

func RunCodeBackup() {
	now := time.Now()
	dateStr := now.Format("060102")

	for _, t := range backupTargets() {
		remoteDir := fmt.Sprintf("%s/%s/%s", backupRemoteBase, dateStr, t.name)

		exec.Command("ssh", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=10",
			fmt.Sprintf("%s@%s", backupDestUser, backupDestHost),
			fmt.Sprintf("mkdir -p %s", remoteDir),
		).Run()

		startTime := time.Now()

		switch {
		case t.isLocal:
			backupLocalToRemote(t.srcDir, remoteDir)
		case t.feisaSelf:
			backupFeisaSelf(t, remoteDir)
		default:
			backupRemoteToRemote(t, remoteDir)
		}

		elapsed := time.Since(startTime).Round(time.Second)
		msg := fmt.Sprintf("[code_backup] %s → feisa:%s (소요 %v)", t.name, remoteDir, elapsed)
		console.Log(msg)
		fmt.Println(msg)
	}

	// Moodle 데이터 백업 (alvinii:/data/moodle/YYMMDD/ → feisa:/mnt/storage/YYMMDD/moodle/)
	backupMoodleData(dateStr)

	// 2주 이상 지난 백업 폴더 삭제
	cleanupOldBackups(now)
}

func backupMoodleData(dateStr string) {
	moodleSrc := fmt.Sprintf("/data/moodle/%s/", dateStr)
	moodleDst := fmt.Sprintf("%s/%s/moodle", backupRemoteBase, dateStr)

	// feisa에 moodle 디렉토리 생성
	exec.Command("ssh", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", backupDestUser, backupDestHost),
		fmt.Sprintf("mkdir -p %s", moodleDst),
	).Run()

	startTime := time.Now()

	// alvinii에서 feisa로 moodle 데이터 rsync
	rsyncScript := fmt.Sprintf(
		"rsync -avz --timeout=600 --exclude='*.log' -e 'ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10' %s %s@%s:%s/",
		moodleSrc, backupDestUser, backupDestHost, moodleDst,
	)
	sshScript := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -p 2222 alvinii@192.168.3.232 '%s'", rsyncScript)

	console.Log("[code_backup] moodle rsync 시작...")
	cmd := exec.Command("bash", "-c", sshScript)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(startTime).Round(time.Second)

	if err != nil {
		console.LogError("[code_backup] moodle rsync 실패: %v (%s)", err, string(out))
	} else {
		console.Log("[code_backup] moodle rsync 완료")
	}
	logRsyncStats(string(out))

	msg := fmt.Sprintf("[code_backup] moodle → feisa:%s (소요 %v)", moodleDst, elapsed)
	console.Log(msg)
	fmt.Println(msg)
}

func backupLocalToRemote(srcDir, remoteDir string) {
	script := fmt.Sprintf(
		"rsync -avz --delete --timeout=600 --exclude=node_modules --exclude=.git --exclude=__pycache__ --exclude=.venv --exclude=test_logs --exclude='*.log' --exclude=data/mariadb -e 'ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10' %s %s@%s:%s/",
		srcDir, backupDestUser, backupDestHost, remoteDir,
	)
	console.Log("[code_backup] white rsync 시작...")
	cmd := exec.Command("bash", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		console.LogError("[code_backup] white rsync 실패: %v (%s)", err, string(out))
	} else {
		console.Log("[code_backup] white rsync 완료")
	}
	logRsyncStats(string(out))
}

func backupFeisaSelf(t backupTarget, remoteDir string) {
	script := fmt.Sprintf(
		"rsync -avz --delete --timeout=600 --exclude=node_modules --exclude=.git --exclude=__pycache__ --exclude=.venv --exclude=test_logs --exclude='*.log' --exclude=data/mariadb %s %s/",
		t.srcDir, remoteDir,
	)
	sshScript := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 %s@%s '%s'",
		t.user, t.host, script)

	console.Log("[code_backup] feisa(자체) rsync 시작...")
	cmd := exec.Command("bash", "-c", sshScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		console.LogError("[code_backup] feisa rsync 실패: %v (%s)", err, string(out))
	} else {
		console.Log("[code_backup] feisa rsync 완료")
	}
	logRsyncStats(string(out))
}

func backupRemoteToRemote(t backupTarget, remoteDir string) {
	rsyncScript := fmt.Sprintf(
		"rsync -avz --delete --timeout=600 --exclude=node_modules --exclude=.git --exclude=__pycache__ --exclude=.venv --exclude=test_logs --exclude='*.log' --exclude=data/mariadb -e 'ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10' %s %s@%s:%s/",
		t.srcDir, backupDestUser, backupDestHost, remoteDir,
	)
	sshScript := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -p %s %s@%s '%s'",
		t.port, t.user, t.host, rsyncScript)

	console.Log("[code_backup] %s rsync 시작...", t.name)
	cmd := exec.Command("bash", "-c", sshScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		console.LogError("[code_backup] %s rsync 실패: %v", t.name, err)
		if len(out) > 0 && len(out) < 500 {
			console.LogError("[code_backup] %s stderr: %s", t.name, string(out))
		}
	} else {
		console.Log("[code_backup] %s rsync 완료", t.name)
	}
	logRsyncStats(string(out))
}


func cleanupOldBackups(now time.Time) {
	cutoff := now.AddDate(0, 0, -14).Format("060102")
	console.Log("[code_backup] 2주 이상 백업 정리 시작 (기준: %s)...", cutoff)

	cleanupCmd := fmt.Sprintf(
		"find %s -maxdepth 1 -type d -name \"[0-9][0-9][0-9][0-9][0-9][0-9]\" | while read d; do dname=$(basename \"$d\"); if [ \"$dname\" -lt \"%s\" ]; then rm -rf \"$d\" && echo \"deleted $dname\"; fi; done",
		backupRemoteBase, cutoff,
	)
	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", backupDestUser, backupDestHost),
		cleanupCmd,
	)
	out, _ := sshCmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "deleted") {
			console.Log("[code_backup] %s", strings.TrimSpace(line))
		}
	}
	console.Log("[code_backup] 백업 정리 완료")
}
func logRsyncStats(output string) {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "sent") && strings.Contains(line, "bytes") {
			console.Log("[code_backup] %s", strings.TrimSpace(line))
		}
	}
}
