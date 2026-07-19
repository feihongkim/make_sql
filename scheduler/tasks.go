package scheduler

import (
	"net/http"
	"bytes"
	"context"
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

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	summary := execStdin(prompt)
	if summary == "" {
		console.LogError("[scheduler] API_pi 요약 실패")
		if err := srv.SendTelegramMsg(output); err != nil {
			console.LogError("[scheduler] 텔레그램 전송 실패: %v", err)
		}
		return
	}
	
	if err := srv.SendTelegramMsg("🛡️ [보안 점검 요약]\n" + summary); err != nil {
		console.LogError("[scheduler] 텔레그램 요약 전송 실패: %v", err)
	}
}
func (s *Scheduler) runSurgeSync() {
	srv.RunSurgeSync()
}

func (s *Scheduler) runTempCheck() {
	srv.RunTempCheck()
}

// --- 프로세스 감시 (watchdog) ---

// 감시 대상 컨테이너 목록
var watchContainers = []string{
	"dart_pi", "drawchart_pi", "jarvis_pi", "kis2_pi", "ls_pi",
	"MC_pi", "pyforme_pi", "pyforme2_pi", "stocktopreason_pi", "youtubeContent_pi",
	"dbsender_pi", "makesql_pi", "mkyoutube_pi", "news_pi",
	"ontology_pi", "restgo_pi", "restgo2_pi", "rstudio_pi", "saver_pi", "upbit_pi",
}

// 컨테이너 내부 프로세스 감시 대상 {컨테이너명: 프로세스 패턴}
var watchInternalProcesses = map[string]string{
	"kis2_pi": "./KIS scheduler",
}

// 호스트 프로세스 감시 대상
var watchHostProcesses = []string{
	"abledb_Hope scheduler",
}


// 요일별 필수 LOG 모듈 (월요일은 15시 이후부터 KIS 체크)
var requiredModules = map[time.Weekday][]string{
	time.Monday:    {"youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
	time.Tuesday:   {"KIS", "youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
	time.Wednesday: {"KIS", "youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
	time.Thursday:  {"KIS", "youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
	time.Friday:    {"KIS", "youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
	time.Saturday:  {"youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
	time.Sunday:    {"youtubeContent", "youtubeList", "MakeSQL", "TopReason"},
}

func checkLogModules(now time.Time) []string {
	var missing []string
	weekday := now.Weekday()
	expected, ok := requiredModules[weekday]
	if !ok {
		return nil
	}

	if weekday == time.Monday && now.Hour() < 15 {
		var filtered []string
		for _, m := range expected {
			if m != "KIS" {
				filtered = append(filtered, m)
			}
		}
		expected = filtered
	}

	if now.Hour() == 0 {
		now = now.Add(-1 * time.Hour) // 00시대에는 전일 데이터로 체크
	}
	today := now.Format("2006/01/02")
	console.Log("[process_check] LOG query: today=%s", today)

	present := queryLogModules(today)
	if present == nil {
		return []string{"LOG module query failed (MongoDB error)"}
	}

	for _, mod := range expected {
		if !present[mod] {
			missing = append(missing, fmt.Sprintf("LOG 모듈 [%s] 오늘 미출현", mod))
		}
	}
	return missing
}

func queryLogModules(dateStr string) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uri := "mongodb://white.tail5b4272.ts.net:27016/?retryWrites=true&w=majority"
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetConnectTimeout(5*time.Second).SetSocketTimeout(5*time.Second))
	if err != nil {
		console.LogError("[process_check] MongoDB connect failed: %v", err)
		return nil
	}
	defer client.Disconnect(ctx)

	coll := client.Database("LOG").Collection(dateStr)

	pipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.D{{Key: "m", Value: bson.D{{Key: "$arrayElemAt", Value: bson.A{bson.D{{Key: "$split", Value: bson.A{"$Msg", "["}}}, 1}}}}}}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$m"}}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		console.LogError("[process_check] MongoDB aggregate failed: %v", err)
		return nil
	}
	defer cursor.Close(ctx)

	result := make(map[string]bool)
	for cursor.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		mod := strings.TrimSuffix(doc.ID, "]")
		if mod != "" {
			result[mod] = true
		}
	}
	return result
}

// execStdin runs docker exec -i API_pi pi -p with the given prompt via stdin
func execStdin(prompt string) string {
	// API_pi HTTP 서버 직접 호출 (Extension)
	reqBody, _ := json.Marshal(map[string]string{"prompt": prompt})
	resp, err := http.Post("http://localhost:3001/run", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		console.LogError("[execStdin] HTTP POST error: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		console.LogError("[execStdin] HTTP status error: %d", resp.StatusCode)
		return ""
	}

	var result struct {
		Output string `json:"output"` 
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		console.LogError("[execStdin] JSON decode error: %v", err)
		return ""
	}
	return result.Output
}

// 이전 알림 상태 (중복 방지)
var lastAlertState = make(map[string]bool)

func (s *Scheduler) runProcessCheck() {
	var issues []string

	// 1. Docker 컨테이너 running 여부 확인
	for _, name := range watchContainers {
		out := strings.TrimSpace(execOutput("docker", []string{"inspect", name, "--format", "{{.State.Status}}"}))
		if out != "running" {
			issues = append(issues, fmt.Sprintf("컨테이너 [%s] 중단됨 (상태: %s)", name, out))
		}
	}

	// 2. 컨테이너 내부 프로세스 확인
	for container, pattern := range watchInternalProcesses {
		// 컨테이너가 running이 아니면 이미 위에서 잡힘
		statusOut := strings.TrimSpace(execOutput("docker", []string{"inspect", container, "--format", "{{.State.Status}}"}))
		if statusOut != "running" {
			continue
		}
		out := execOutput("docker", []string{"exec", container, "bash", "-c", fmt.Sprintf("cat /proc/*/cmdline 2>/dev/null | tr '\\0' ' ' | grep -q '%s' && echo found || echo notfound", pattern)})
		if strings.TrimSpace(out) != "found" {
			issues = append(issues, fmt.Sprintf("컨테이너 [%s] 내부 프로세스 없음: %s", container, pattern))
		}
	}

	// 3. 호스트 프로세스 확인
	for _, pattern := range watchHostProcesses {
		out := execOutput("bash", []string{"-c", fmt.Sprintf("ps aux | grep '%s' | grep -v grep", pattern)})
		if strings.TrimSpace(out) == "" {
			issues = append(issues, fmt.Sprintf("호스트 프로세스 없음: %s", pattern))
		}
	}

	// 4. LOG 모듈 요일별 감시
	now := time.Now().In(loc)
	moduleIssues := checkLogModules(now)
	issues = append(issues, moduleIssues...)

	// 이슈 상태 변화 감지
	currentKey := strings.Join(issues, "|")
	prevKey := ""
	for k, v := range lastAlertState {
		if v {
			prevKey = k
		}
	}

	if len(issues) == 0 {
		if prevKey != "" {
			// 복구됨
			lastAlertState[prevKey] = false
			if err := srv.SendTelegramMsg("✅ [프로세스 감시] 모든 이상 복구됨"); err != nil {
				console.LogError("[scheduler] process-check 텔레그램 전송 실패: %v", err)
			}
		}
		console.Log("[scheduler] process-check: 전체 정상")
		return
	}

	// 이전과 동일한 이슈면 알림 생략
	if currentKey == prevKey {
		console.Log("[scheduler] process-check: 이전과 동일한 이슈 — 알림 생략")
		return
	}

	// 새 이슈 발생
	lastAlertState[prevKey] = false
	lastAlertState[currentKey] = true

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("⚠️ [프로세스 감시] %d건 이상 감지\n\n", len(issues)))
	for _, issue := range issues {
		msg.WriteString("• " + issue + "\n")
	}
	msg.WriteString(fmt.Sprintf("\n점검 시각: %s", time.Now().In(loc).Format("2006-01-02 15:04 KST")))

	if err := srv.SendTelegramMsg(msg.String()); err != nil {
		console.LogError("[scheduler] process-check 텔레그램 전송 실패: %v", err)
	}
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

func (s *Scheduler) runCodeBackup() {
	srv.RunCodeBackup()
}
