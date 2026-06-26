package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"MakeSQL/console"
	"MakeSQL/srv"
)

// --- Task 구현 ---

func (s *Scheduler) runNginxAnalyze() {
	self, err := os.Executable()
	if err != nil {
		console.LogError("[scheduler] 실행 경로 확인 실패: %v", err)
		return
	}
	scriptDir := fmt.Sprintf("%s/nginx_log", getExecDir(self))
	output := execOutput("bash", []string{scriptDir + "/fetch_and_analyze.sh", "3"})
	if output == "" {
		console.Log("[scheduler] nginx-analyze 결과 없음")
		return
	}
	if err := srv.SendTelegramMsg(formatNginxAnalyzeMsg(output)); err != nil {
		console.LogError("[scheduler] 텔레그램 전송 실패: %v", err)
	}
}

func (s *Scheduler) runLogAnalyze(args []string) {
	self, err := os.Executable()
	if err != nil {
		console.LogError("[scheduler] 실행 경로 확인 실패: %v", err)
		return
	}
	output := execOutput(self, append([]string{"log-analyze"}, args...))
	if output == "" {
		console.Log("[scheduler] log-analyze 결과 없음")
		return
	}
	if err := srv.SendTelegramMsg(formatLogAnalyzeMsg(output)); err != nil {
		console.LogError("[scheduler] 텔레그램 전송 실패: %v", err)
	}
}

func (s *Scheduler) runBlogSync() {
	output := execOutput(
		"/home/feihong/code/blog/.venv/bin/python3",
		[]string{"/home/feihong/code/blog/main.py", "post", "sync"},
	)
	f, err := os.OpenFile("/home/feihong/code/blog/sync.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(output + "\n")
		f.Close()
	}
}

func (s *Scheduler) runSecurityCheck() {
	output := srv.HandleSecurityCheck()
	if output == "" {
		console.Log("[scheduler] security-check 결과 없음")
		return
	}
	prompt := "다음 서버 보안 점검 결과를 핵심 이슈 위주로 간결하게 한국어로 요약해줘. 정상 항목은 생략하고 주의/조치 필요한 것만:\n\n" + output
	summary := execOutput("docker", []string{"exec", "makesql_claude", "claude", "-p", prompt})
	msg := summary
	if msg == "" {
		console.LogError("[scheduler] makesql_claude 요약 실패, 원본 전송")
		msg = output
	}
	if err := srv.SendTelegramMsg(msg); err != nil {
		console.LogError("[scheduler] 텔레그램 전송 실패: %v", err)
	}
}

func (s *Scheduler) runSurgeSync() {
	srv.RunSurgeSync()
}

func (s *Scheduler) runTempCheck() {
	srv.RunTempCheck()
}

func (s *Scheduler) runTgMonitor() {
	execOutput("/home/feihong/code/MakeSQL/python/.venv/bin/python3", []string{"/home/feihong/code/MakeSQL/python/tg_monitor.py"})
}

func (s *Scheduler) runYoutubeList() {
	execOutputDir("/home/feihong/code/youtubeList", "/home/feihong/code/youtubeList/youtubeList", nil)
}

func (s *Scheduler) runQueueToMongo() {
	srv.RunQueueToMongo(16) // KST 16시에 자동 종료
}

func (s *Scheduler) runTopReasonAnalyze() {
	execOutputDir("/home/feihong/code/StockTopReason", "/home/feihong/code/StockTopReason/TopReason_Hope", []string{"--mode", "all"})
}

func (s *Scheduler) runYoutubeContent() {
	execOutput("ssh", []string{
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no",
		"feivy@100.124.181.80",
		`Set-Location C:\Users\feivy\code\youtubeContent; .\youtubeContent.exe`,
	})
}

// --- 서브프로세스 ---

func execOutputDir(dir, bin string, args []string) string {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	done := make(chan error, 1)
	var out []byte
	var cmdErr error
	go func() {
		out, cmdErr = cmd.Output()
		done <- cmdErr
	}()
	select {
	case <-done:
		if cmdErr != nil {
			console.LogError("[subprocess] 실행 오류: %v", cmdErr)
		}
		return string(out)
	case <-time.After(10 * time.Minute):
		cmd.Process.Kill()
		console.LogError("[subprocess] 타임아웃 (10분)")
		return ""
	}
}

func execOutput(bin string, args []string) string {
	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	done := make(chan error, 1)
	var out []byte
	var cmdErr error
	go func() {
		out, cmdErr = cmd.Output()
		done <- cmdErr
	}()
	select {
	case <-done:
		if cmdErr != nil {
			console.LogError("[subprocess] 실행 오류: %v", cmdErr)
		}
		return string(out)
	case <-time.After(10 * time.Minute):
		cmd.Process.Kill()
		console.LogError("[subprocess] 타임아웃 (10분)")
		return ""
	}
}

func getExecDir(self string) string {
	for i := len(self) - 1; i >= 0; i-- {
		if self[i] == '/' {
			return self[:i]
		}
	}
	return "."
}

// --- Nginx 분석 포맷 ---

type NginxAnalyzeResult struct {
	PeriodHours   int `json:"period_hours"`
	TotalRequests int `json:"total_requests"`
	Servers       map[string]struct {
		Requests  int            `json:"requests"`
		UniqueIPs int            `json:"unique_ips"`
		Bots      int            `json:"bots"`
		BotPct    float64        `json:"bot_pct"`
		Status    map[string]int `json:"status"`
	} `json:"servers"`
	Security struct {
		ThreatTotal  int            `json:"threat_total"`
		ThreatByType map[string]int `json:"threat_by_type"`
		ThreatTopIPs map[string]int `json:"threat_top_ips"`
	} `json:"security"`
	Fail2Ban struct {
		Bans   int `json:"bans"`
		Unbans int `json:"unbans"`
	} `json:"fail2ban"`
	Errors struct {
		Total int `json:"total"`
	} `json:"errors"`
}

func formatNginxAnalyzeMsg(jsonStr string) string {
	if idx := strings.Index(jsonStr, "{"); idx > 0 {
		jsonStr = jsonStr[idx:]
	}
	var r NginxAnalyzeResult
	if err := json.Unmarshal([]byte(jsonStr), &r); err != nil {
		return "[nginx-analyze] JSON 파싱 실패: " + err.Error()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🛡 Nginx 보안 분석 (최근 %dh, 총 %s건)\n\n", r.PeriodHours, commaInt(r.TotalRequests)))
	for _, name := range []string{"feivyblog", "moodle", "unknown"} {
		sv, ok := r.Servers[name]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("【%s】요청:%s 봇:%.1f%%\n", name, commaInt(sv.Requests), sv.BotPct))
	}
	b.WriteString("\n")
	if r.Security.ThreatTotal > 0 {
		b.WriteString(fmt.Sprintf("⚠️ 위협 %d건\n", r.Security.ThreatTotal))
		for typ, cnt := range r.Security.ThreatByType {
			b.WriteString(fmt.Sprintf("  %s: %d건\n", typ, cnt))
		}
		if len(r.Security.ThreatTopIPs) > 0 {
			b.WriteString("  위협 IP: ")
			for ip, cnt := range r.Security.ThreatTopIPs {
				b.WriteString(fmt.Sprintf("%s(%d) ", ip, cnt))
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("✅ 보안 위협 없음\n")
	}
	b.WriteString(fmt.Sprintf("🔒 Fail2Ban: 밴 %d건 / 해제 %d건\n", r.Fail2Ban.Bans, r.Fail2Ban.Unbans))
	if r.Errors.Total > 0 {
		b.WriteString(fmt.Sprintf("🔴 에러로그 %d건\n", r.Errors.Total))
	}
	return strings.TrimSpace(b.String())
}

// --- Log 분석 포맷 ---

func formatLogAnalyzeMsg(jsonStr string) string {
	if idx := strings.Index(jsonStr, "{"); idx > 0 {
		jsonStr = jsonStr[idx:]
	}
	var r srv.LogAnalyzeResult
	if err := json.Unmarshal([]byte(jsonStr), &r); err != nil {
		return "[log-analyze] JSON 파싱 실패: " + err.Error()
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📊 White LOG 분석 (%s ~ %s)\n\n", r.Range.From[11:16], r.Range.To[11:16]))

	errMark := "✅"
	if r.Summary.Errors > 0 {
		errMark = "🔴"
	}
	b.WriteString("【전체 요약】\n")
	b.WriteString(fmt.Sprintf("  전체 로그: %s건\n", commaInt(r.Summary.TotalLogs)))
	b.WriteString(fmt.Sprintf("  API 실패: %d건 (%s)\n", r.Summary.APIFailures, r.Summary.APIFailureRate))
	b.WriteString(fmt.Sprintf("  ERROR: %d건 %s\n\n", r.Summary.Errors, errMark))

	if len(r.Modules) > 0 {
		type kv struct{ k string; v int }
		var mods []kv
		for k, v := range r.Modules {
			mods = append(mods, kv{k, v})
		}
		sort.Slice(mods, func(i, j int) bool { return mods[i].v > mods[j].v })
		b.WriteString("【모듈별 로그 분포】\n")
		for _, m := range mods {
			b.WriteString(fmt.Sprintf("  %s: %s건\n", m.k, commaInt(m.v)))
		}
		b.WriteString("\n")
	}

	if r.APIFailures.Total > 0 {
		b.WriteString(fmt.Sprintf("【API 실패 현황 (%d건)】\n", r.APIFailures.Total))
		b.WriteString(fmt.Sprintf("  http500 %d건 / timeout %d건 / 기타 %d건\n",
			r.APIFailures.ByReason.HTTP500, r.APIFailures.ByReason.Timeout, r.APIFailures.ByReason.Other))
		type kv struct{ k string; v int }
		var tasks []kv
		for k, v := range r.APIFailures.ByTask {
			tasks = append(tasks, kv{k, v})
		}
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].v > tasks[j].v })
		for _, t := range tasks {
			b.WriteString(fmt.Sprintf("  · %s: %d건\n", t.k, t.v))
		}
		if r.Summary.Errors == 0 {
			b.WriteString("  → 재시도를 통해 전량 복구 완료\n")
		}
		b.WriteString("\n")
	}

	if len(r.KISCompletions) > 0 {
		taskMap := make(map[string]string)
		taskOrder := []string{}
		reTask := regexp.MustCompile(`\[([A-Z]{2}\.[A-Z_a-z]+)\]`)
		for _, comp := range r.KISCompletions {
			m := reTask.FindStringSubmatch(comp)
			if m == nil {
				continue
			}
			name := m[1]
			if _, exists := taskMap[name]; !exists {
				taskOrder = append(taskOrder, name)
			}
			taskMap[name] = comp
		}
		b.WriteString(fmt.Sprintf("【KIS 파이프라인 완료 (%d개 태스크)】\n", len(taskOrder)))
		reStd := regexp.MustCompile(`성공=(\d+), 실패=(\d+), 스킵=(\d+), MERGE=(\d+)건`)
		reRows := regexp.MustCompile(`(\d+) rows merged \((\d+) pages\)`)
		reRows2 := regexp.MustCompile(`(\d+) rows merged`)
		reRetry := regexp.MustCompile(`재시도 완료: (\d+)/(\d+)건 복구`)
		for _, name := range taskOrder {
			comp := taskMap[name]
			if m := reStd.FindStringSubmatch(comp); m != nil {
				ok, _ := strconv.Atoi(m[1])
				skip, _ := strconv.Atoi(m[3])
				merge, _ := strconv.Atoi(m[4])
				b.WriteString(fmt.Sprintf("  %s | 성공 %s 스킵 %s MERGE %s건\n",
					name, commaInt(ok), commaInt(skip), commaInt(merge)))
			} else if m := reRows.FindStringSubmatch(comp); m != nil {
				rows, _ := strconv.Atoi(m[1])
				pages, _ := strconv.Atoi(m[2])
				b.WriteString(fmt.Sprintf("  %s | %s건 (%s pages)\n", name, commaInt(rows), commaInt(pages)))
			} else if m := reRows2.FindStringSubmatch(comp); m != nil {
				rows, _ := strconv.Atoi(m[1])
				b.WriteString(fmt.Sprintf("  %s | %s건 merged\n", name, commaInt(rows)))
			} else if m := reRetry.FindStringSubmatch(comp); m != nil {
				b.WriteString(fmt.Sprintf("  %s | 재시도 복구 %s/%s건\n", name, m[1], m[2]))
			} else {
				short := comp
				if i := strings.Index(comp, "] 완료"); i > 0 {
					short = comp[i+2:]
				}
				if len(short) > 60 {
					short = short[:60]
				}
				b.WriteString(fmt.Sprintf("  %s | %s\n", name, strings.TrimSpace(short)))
			}
		}
		b.WriteString("\n")
	}

	ks := r.KISScheduler
	if len(ks.Completed)+len(ks.Running)+len(ks.Failed) > 0 {
		b.WriteString("【KIS 스케줄러 현황】\n")
		if len(ks.Completed) > 0 {
			b.WriteString("  완료: " + strings.Join(ks.Completed, ", ") + "\n")
		}
		if len(ks.Running) > 0 {
			b.WriteString("  실행중: " + strings.Join(ks.Running, ", ") + "\n")
		}
		if len(ks.Failed) > 0 {
			b.WriteString("  실패: " + strings.Join(ks.Failed, ", ") + "\n")
		}
		b.WriteString("\n")
	}

	if r.News.Total > 0 {
		b.WriteString(fmt.Sprintf("【뉴스 수집 현황】총 %s건\n", commaInt(r.News.Total)))
		type newsCycle struct {
			time       string
			total, ok int
		}
		var cycles []newsCycle
		totalOK, totalFill := 0, 0
		for _, detail := range r.News.Details {
			idx1 := strings.Index(detail, "완료: ")
			idx2 := strings.Index(detail, "건 중 ")
			idx3 := strings.Index(detail, "건 본문 채우기 성공")
			if idx1 < 0 || idx2 < 0 || idx3 < 0 {
				continue
			}
			tot, err1 := strconv.Atoi(strings.TrimSpace(detail[idx1+len("완료: ") : idx2]))
			ok, err2 := strconv.Atoi(strings.TrimSpace(detail[idx2+len("건 중 ") : idx3]))
			if err1 != nil || err2 != nil {
				continue
			}
			t := ""
			if len(detail) >= 16 {
				t = detail[11:16]
			}
			cycles = append(cycles, newsCycle{t, tot, ok})
			totalOK += ok
			totalFill += tot
		}
		for i, c := range cycles {
			rate := 0
			if c.total > 0 {
				rate = c.ok * 100 / c.total
			}
			b.WriteString(fmt.Sprintf("  [%d차] %s | 수집 %d건 → 본문 성공 %d건(%d%%) 실패 %d건\n",
				i+1, c.time, c.total, c.ok, rate, c.total-c.ok))
		}
		if len(cycles) > 1 && totalFill > 0 {
			overallRate := totalOK * 100 / totalFill
			b.WriteString(fmt.Sprintf("  합계: %d건 수집 / 본문 성공 %d건(%d%%)\n", totalFill, totalOK, overallRate))
		}
		b.WriteString("\n")
	}

	b.WriteString("【AI 분석 현황】\n")
	if r.AIAnalysis.Total > 0 {
		b.WriteString(fmt.Sprintf("  분석 완료: %s건\n\n", commaInt(r.AIAnalysis.Total)))
	} else {
		b.WriteString("  분석 완료: 0건 (해당 시간대 미실행)\n\n")
	}

	b.WriteString("【종합 평가】\n")
	if r.Summary.Errors > 0 {
		b.WriteString(fmt.Sprintf("  🔴 ERROR %d건 발생 — 즉시 확인 필요\n", r.Summary.Errors))
	} else if r.APIFailures.Total > 0 {
		b.WriteString(fmt.Sprintf("  ✅ 정상 운영 중. API 실패 %d건 발생했으나 재시도 복구 완료.", r.APIFailures.Total))
		if len(ks.Completed) > 0 {
			b.WriteString(fmt.Sprintf(" KIS %d배치 완료.", len(ks.Completed)))
		}
		if r.News.Total > 0 {
			b.WriteString(" 뉴스 수집 정상.")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("  ✅ 전체 정상 — API 실패 0건, ERROR 0건.")
		if len(ks.Completed) > 0 {
			b.WriteString(fmt.Sprintf(" KIS %d배치 완료.", len(ks.Completed)))
		}
		if r.News.Total > 0 {
			b.WriteString(" 뉴스 수집 정상.")
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func commaInt(n int) string {
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}
