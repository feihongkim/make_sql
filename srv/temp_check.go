package srv

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"MakeSQL/console"
)

type serverTemp struct {
	name string
	cpu  float64
	nvme float64
	err  string
}

// RunTempCheck 는 CLI 및 스케줄러에서 호출 가능한 temp-check 진입점이다.
func RunTempCheck() {
	results := []serverTemp{
		getTempLocal("white"),
		getTempRemote("tuf(130)", "feihong", "192.168.3.130", "22"),
		getTempRemote("alvinii(232)", "alvinii", "192.168.3.232", "2222"),
	}

	var b strings.Builder
	b.WriteString("🌡 서버 온도 현황\n\n")

	warn := false
	for _, r := range results {
		if r.err != "" {
			b.WriteString(fmt.Sprintf("【%s】접속 실패: %s\n", r.name, r.err))
			warn = true
			continue
		}
		cpuIcon := "🟢"
		if r.cpu >= 80 {
			cpuIcon = "🔴"
			warn = true
		} else if r.cpu >= 65 {
			cpuIcon = "🟡"
		}
		nvmePart := ""
		if r.nvme > 0 {
			nvmeIcon := "🟢"
			if r.nvme >= 65 {
				nvmeIcon = "🔴"
				warn = true
			} else if r.nvme >= 58 {
				nvmeIcon = "🟡"
			}
			nvmePart = fmt.Sprintf("  NVMe %s%.1f°C", nvmeIcon, r.nvme)
		}
		b.WriteString(fmt.Sprintf("【%s】CPU %s%.1f°C%s\n",
			r.name, cpuIcon, r.cpu, nvmePart))
	}

	if warn {
		b.WriteString("\n⚠️ 고온 경고 — 즉시 확인 필요")
	} else {
		b.WriteString("\n✅ 전체 정상 범위")
	}

	if err := SendTelegramMsg(strings.TrimSpace(b.String())); err != nil {
		console.LogError("[temp_check] 텔레그램 전송 실패: %v", err)
	}
}

func getTempLocal(name string) serverTemp {
	out, err := exec.Command("sensors").Output()
	if err != nil {
		return serverTemp{name: name, err: err.Error()}
	}
	return parseSensors(name, string(out))
}

func getTempRemote(name, user, host, port string) serverTemp {
	cmd := `echo CPU:$(cat /sys/class/thermal/thermal_zone0/temp 2>/dev/null || echo 0); NVME_TEMP=0; for d in /sys/class/hwmon/hwmon*; do n=$(cat "$d/name" 2>/dev/null); if [ "$n" = "nvme" ]; then t=$(cat "$d/temp1_input" 2>/dev/null); [ "$t" -gt "$NVME_TEMP" ] 2>/dev/null && NVME_TEMP=$t; fi; done; echo NVME:$NVME_TEMP`
	out, err := exec.Command("ssh",
		"-p", port,
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", user, host),
		cmd,
	).Output()
	if err != nil {
		return serverTemp{name: name, err: err.Error()}
	}
	return parseSysfs(name, string(out))
}

func parseSensors(name, out string) serverTemp {
	t := serverTemp{name: name}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Tctl:") {
			t.cpu = parseTemp(line)
		}
		if strings.HasPrefix(line, "Composite:") {
			t.nvme = parseTemp(line)
		}
	}
	return t
}

func parseSysfs(name, out string) serverTemp {
	t := serverTemp{name: name}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			continue
		}
		celsius := val / 1000.0
		switch parts[0] {
		case "CPU":
			t.cpu = celsius
		case "NVME":
			t.nvme = celsius
		}
	}
	return t
}

func parseTemp(line string) float64 {
	// "+58.6°C" 형태에서 숫자 추출
	start := strings.Index(line, "+")
	if start < 0 {
		return 0
	}
	end := strings.Index(line[start:], "°")
	if end < 0 {
		return 0
	}
	val, _ := strconv.ParseFloat(line[start+1:start+end], 64)
	return val
}
