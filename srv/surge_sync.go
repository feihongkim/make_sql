package srv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"MakeSQL/console"
)

const (
	surgeDeployUser   = "alvinii"
	surgeDeployHost   = "192.168.3.232"
	surgeDeployPort   = "2222"
	surgeDeployPath   = "/home/alvinii/code/my-blog/data/blog/StockItems"
	surgeSSHKey       = "/root/.ssh/id_ed25519"
	surgeLookbackDays = 7
)

// RunSurgeSync 는 CLI 및 스케줄러에서 호출 가능한 surge-sync 진입점이다.
func RunSurgeSync() {
	since := time.Now().AddDate(0, 0, -surgeLookbackDays).Format("20060102")

	rows, err := console.QueryMSSQLRows("white", "News", fmt.Sprintf(
		"SELECT DISTINCT CONVERT(VARCHAR(8), stck_bsop_date, 112) AS dt FROM SurgeAnalysis WHERE stck_bsop_date >= '%s' ORDER BY dt",
		since,
	))
	if err != nil {
		console.LogError("[surge_sync] DB 조회 실패: %v", err)
		return
	}

	existing := listRemoteFiles()

	tmpDir := fmt.Sprintf("/tmp/surge_sync_%s", time.Now().Format("20060102150405"))
	defer os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		console.LogError("[surge_sync] 임시 디렉토리 생성 실패: %v", err)
		return
	}

	var generated []string
	for _, row := range rows {
		dt := strings.TrimSpace(row["dt"])
		if dt == "" || existing[dt] {
			continue
		}
		generateAndSave("white", dt, dt, tmpDir, "mdx")
		generated = append(generated, dt)
	}

	if len(generated) == 0 {
		console.Log("[surge_sync] 누락 파일 없음 — 동기화 완료")
		return
	}

	deploy := fmt.Sprintf("%s@%s:%s:%s", surgeDeployUser, surgeDeployHost, surgeDeployPort, surgeDeployPath)
	deployFiles(tmpDir, deploy, "mdx")

	msg := fmt.Sprintf("[surge_sync] %d개 MDX 신규 배포: %s", len(generated), strings.Join(generated, ", "))
	fmt.Println(msg)
	if err := SendTelegramMsg(msg); err != nil {
		console.LogError("[surge_sync] 텔레그램 전송 실패: %v", err)
	}
}

// listRemoteFiles 는 alvinii StockItems 폴더의 기존 파일명을 "YYYYMMDD" 키 셋으로 반환한다.
func listRemoteFiles() map[string]bool {
	out, err := exec.Command("ssh",
		"-i", surgeSSHKey,
		"-p", surgeDeployPort,
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", surgeDeployUser, surgeDeployHost),
		fmt.Sprintf("ls %s/", surgeDeployPath),
	).Output()

	result := make(map[string]bool)
	if err != nil {
		console.LogError("[surge_sync] 원격 파일 목록 조회 실패: %v", err)
		return result
	}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSuffix(strings.TrimSpace(line), ".mdx")
		if len(name) == 6 {
			result["20"+name] = true
		}
	}
	return result
}
