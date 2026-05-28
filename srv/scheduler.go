package srv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	telegramBotToken   = "8678638419:AAHqimHvEH1Lt6bXe1CFVWu5FzPDWelTuKQ"
	telegramChatID     = "7723743534"
	schedulerWorkspace = "/home/feihong/code/SchedulerWorkspace"
)

// sendTelegramMsg sends a message directly to Telegram via Bot API (no MCP, no plugin).
func sendTelegramMsg(text string) error {
	apiURL := "https://api.telegram.org/bot" + telegramBotToken + "/sendMessage"
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id": {telegramChatID},
		"text":    {text},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API 오류: %s", string(body))
	}
	return nil
}

// morning_briefing, todo_copy 태스크는 /schedule 원격 루틴으로 이전됨:
// - morning_briefing: trig_01HKH8KAF4F9C2HRMbR3ga6D (매일 KST 08:00)
// - todo_copy:        trig_01Jod4W84Cskbt5YZ6wwnZS7 (매일 KST 08:00)

var loc *time.Location

func init() {
	loc, _ = time.LoadLocation("Asia/Seoul")
}

// Task는 스케줄 단위 작업을 정의한다.
type Task struct {
	Label     string
	Time      string               // "HH:MM" 실행 시각 (Cron이 아닌 경우)
	Every     int                  // 반복 간격 (시간 단위, 0이면 Time 기반)
	AtMinute  int                  // Every 사용 시 실행 분 (예: 40 → 매 N시간 40분)
	Commands  []string             // 실행할 서브커맨드
	Condition func(time.Time) bool // 실행 조건 (nil이면 항상)
}

// BuildSchedule 은 스케줄 목록을 구성한다.
func BuildSchedule() []Task {
	return []Task{
		{
			Label:    "log_analyze",
			Every:    3,
			AtMinute: 40,
			Commands: []string{"log-analyze", "white", "3"},
		},
		{
			Label:    "nginx_analyze",
			Every:    3,
			AtMinute: 10,
			Commands: []string{"nginx-analyze"},
		},
		{
			Label:    "security_check",
			Every:    3,
			AtMinute: 30,
			Commands: []string{"security-check"},
		},
	}
}

// Scheduler 는 스케줄러 상태를 관리한다.
type Scheduler struct {
	tasks     []Task
	completed map[string]time.Time // "YYYYMMDD:label" → 완료 시각
	today     string
	stopCh    chan struct{}
	pidFile   string
}

func newScheduler() *Scheduler {
	return &Scheduler{
		tasks:     BuildSchedule(),
		completed: make(map[string]time.Time),
		today:     time.Now().In(loc).Format("20060102"),
		stopCh:    make(chan struct{}),
		pidFile:   "test_logs/scheduler.pid",
	}
}

// HandleScheduler 는 scheduler 서브커맨드를 처리한다.
func HandleScheduler(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "status":
			schedulerStatus()
			return
		case "stop":
			schedulerStop()
			return
		}
	}

	s := newScheduler()
	s.run()
}

func (s *Scheduler) run() {
	// PID 파일로 중복 실행 방지
	if isSchedulerRunning(s.pidFile) {
		fmt.Println("[scheduler] 이미 실행 중입니다.")
		os.Exit(1)
	}
	writePID(s.pidFile)
	defer os.Remove(s.pidFile)

	fmt.Println("[scheduler] 스케줄러 시작")
	fmt.Println("[scheduler] 등록된 태스크:")
	for _, t := range s.tasks {
		if t.Every > 0 {
			fmt.Printf("  - %s: 매 %d시간 %d분에 실행\n", t.Label, t.Every, t.AtMinute)
		} else {
			fmt.Printf("  - %s: 매일 %s 실행\n", t.Label, t.Time)
		}
	}

	// SIGTERM graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Println("\n[scheduler] 종료 시그널 수신")
		close(s.stopCh)
	}()

	// 30초마다 tick
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	s.tick() // 초기 체크

	for {
		select {
		case <-s.stopCh:
			fmt.Println("[scheduler] 스케줄러 종료")
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	now := time.Now().In(loc)
	day := now.Format("20060102")

	// 자정 넘기면 completed 리셋
	if day != s.today {
		s.completed = make(map[string]time.Time)
		s.today = day
		fmt.Printf("[scheduler] 날짜 변경: %s\n", day)
	}

	for _, task := range s.tasks {
		if s.shouldRun(task, now) {
			s.dispatch(task, now)
		}
	}
}

func (s *Scheduler) shouldRun(task Task, now time.Time) bool {
	key := s.completedKey(task, now)

	// 이미 완료?
	if _, done := s.completed[key]; done {
		return false
	}

	// 조건 확인
	if task.Condition != nil && !task.Condition(now) {
		return false
	}

	if task.Every > 0 {
		// 반복 태스크: 매 N시간 AtMinute분에 실행
		hour := now.Hour()
		if hour%task.Every != 0 {
			return false
		}
		return inMinuteWindow(task.AtMinute, now)
	}

	// 고정 시간 태스크: 2분 윈도우
	return inTimeWindow(task.Time, now)
}

// completedKey 는 반복 태스크의 경우 시간대별, 고정은 일별 키를 생성한다.
func (s *Scheduler) completedKey(task Task, now time.Time) string {
	if task.Every > 0 {
		return fmt.Sprintf("%s:%s:%02d", s.today, task.Label, now.Hour())
	}
	return s.today + ":" + task.Label
}

func (s *Scheduler) dispatch(task Task, now time.Time) {
	key := s.completedKey(task, now)
	s.completed[key] = now

	fmt.Printf("[scheduler] [%s] %s 실행 시작\n", now.Format("15:04:05"), task.Label)

	go func() {
		switch task.Commands[0] {
		case "nginx-analyze":
			s.runNginxAnalyze()
		case "log-analyze":
			s.runLogAnalyze(task.Commands[1:])
		case "security-check":
			s.runSecurityCheck()
		default:
			fmt.Printf("[scheduler] 알 수 없는 명령: %s\n", task.Commands[0])
		}
		fmt.Printf("[scheduler] [%s] %s 완료\n",
			time.Now().In(loc).Format("15:04:05"), task.Label)
	}()
}


func (s *Scheduler) runNginxAnalyze() {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("[scheduler] 실행 경로 확인 실패: %v\n", err)
		return
	}

	// 실행 파일 기준 nginx_log 폴더
	scriptDir := fmt.Sprintf("%s/nginx_log", getExecDir(self))
	fetchScript := scriptDir + "/fetch_and_analyze.sh"

	output := import_exec_command_output("bash", []string{fetchScript, "2"})
	if output == "" {
		fmt.Println("[scheduler] nginx-analyze 결과 없음")
		return
	}

	// JSON 파싱 후 Go에서 직접 포맷 (claude -p 호출 없음)
	msg := formatNginxAnalyzeMsg(output)
	if err := sendTelegramMsg(msg); err != nil {
		fmt.Printf("[scheduler] 텔레그램 전송 실패: %v\n", err)
	}
}

// NginxAnalyzeResult 는 nginx 분석 JSON 최소 구조
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
	idx := strings.Index(jsonStr, "{")
	if idx > 0 {
		jsonStr = jsonStr[idx:]
	}

	var r NginxAnalyzeResult
	if err := json.Unmarshal([]byte(jsonStr), &r); err != nil {
		return "[nginx-analyze] JSON 파싱 실패: " + err.Error()
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🛡 Nginx 보안 분석 (최근 %dh, 총 %s건)\n\n", r.PeriodHours, commaInt(r.TotalRequests)))

	for _, srv := range []string{"feivyblog", "moodle", "unknown"} {
		s, ok := r.Servers[srv]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("【%s】요청:%s 봇:%.1f%%\n", srv, commaInt(s.Requests), s.BotPct))
	}
	b.WriteString("\n")

	if r.Security.ThreatTotal > 0 {
		b.WriteString(fmt.Sprintf("⚠️ 위협 %d건\n", r.Security.ThreatTotal))
		for typ, cnt := range r.Security.ThreatByType {
			b.WriteString(fmt.Sprintf("  %s: %d건\n", typ, cnt))
		}
		if len(r.Security.ThreatTopIPs) > 0 {
			b.WriteString("  위협 IP: ")
			i := 0
			for ip, cnt := range r.Security.ThreatTopIPs {
				if i >= 3 {
					break
				}
				b.WriteString(fmt.Sprintf("%s(%d) ", ip, cnt))
				i++
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

func getExecDir(self string) string {
	dir := self
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' {
			return dir[:i]
		}
	}
	return "."
}


func (s *Scheduler) runLogAnalyze(args []string) {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("[scheduler] 실행 경로 확인 실패: %v\n", err)
		return
	}

	analyzeArgs := append([]string{"log-analyze"}, args...)
	output := runSubprocessOutput(self, analyzeArgs)

	if output == "" {
		fmt.Println("[scheduler] log-analyze 결과 없음")
		return
	}

	// JSON 파싱 후 Go에서 직접 포맷 (claude -p 호출 없음)
	msg := formatLogAnalyzeMsg(output)
	if err := sendTelegramMsg(msg); err != nil {
		fmt.Printf("[scheduler] 텔레그램 전송 실패: %v\n", err)
	}
}

// formatLogAnalyzeMsg 는 log-analyze JSON을 Telegram 메시지로 포맷한다.
func formatLogAnalyzeMsg(jsonStr string) string {
	idx := strings.Index(jsonStr, "{")
	if idx > 0 {
		jsonStr = jsonStr[idx:]
	}
	var r LogAnalyzeResult
	if err := json.Unmarshal([]byte(jsonStr), &r); err != nil {
		return "[log-analyze] JSON 파싱 실패: " + err.Error()
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📊 White LOG 분석 (%s ~ %s)\n\n", r.Range.From[11:16], r.Range.To[11:16]))

	// 전체 요약
	errMark := "✅"
	if r.Summary.Errors > 0 {
		errMark = "🔴"
	}
	b.WriteString("【전체 요약】\n")
	b.WriteString(fmt.Sprintf("  전체 로그: %s건\n", commaInt(r.Summary.TotalLogs)))
	b.WriteString(fmt.Sprintf("  API 실패: %d건 (%s)\n", r.Summary.APIFailures, r.Summary.APIFailureRate))
	b.WriteString(fmt.Sprintf("  ERROR: %d건 %s\n\n", r.Summary.Errors, errMark))

	// 모듈별 로그 분포 (전체)
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

	// API 실패 현황
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

	// KIS 파이프라인 완료 현황
	if len(r.KISCompletions) > 0 {
		// 태스크명 → 마지막 완료 메시지로 중복 제거
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
				b.WriteString(fmt.Sprintf("  %s | %s건 (%s pages)\n",
					name, commaInt(rows), commaInt(pages)))
			} else if m := reRows2.FindStringSubmatch(comp); m != nil {
				rows, _ := strconv.Atoi(m[1])
				b.WriteString(fmt.Sprintf("  %s | %s건 merged\n", name, commaInt(rows)))
			} else if m := reRetry.FindStringSubmatch(comp); m != nil {
				b.WriteString(fmt.Sprintf("  %s | 재시도 복구 %s/%s건\n", name, m[1], m[2]))
			} else {
				// 그 외: 원문 요약
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

	// KIS 스케줄러 현황
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

	// 뉴스 수집 현황
	if r.News.Total > 0 {
		b.WriteString(fmt.Sprintf("【뉴스 수집 현황】총 %s건\n", commaInt(r.News.Total)))
		type newsCycle struct{ time string; total, ok int }
		var cycles []newsCycle
		totalOK, totalFill := 0, 0
		for _, detail := range r.News.Details {
			idx1 := strings.Index(detail, "완료: ")
			idx2 := strings.Index(detail, "건 중 ")
			idx3 := strings.Index(detail, "건 본문 채우기 성공")
			if idx1 < 0 || idx2 < 0 || idx3 < 0 {
				continue
			}
			tot, err1 := strconv.Atoi(strings.TrimSpace(detail[idx1+len("완료: "):idx2]))
			ok, err2 := strconv.Atoi(strings.TrimSpace(detail[idx2+len("건 중 "):idx3]))
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
			b.WriteString(fmt.Sprintf("  합계: %d건 수집 / 본문 성공 %d건(%d%%)\n",
				totalFill, totalOK, overallRate))
		}
		b.WriteString("\n")
	}

	// AI 분석 현황
	b.WriteString("【AI 분석 현황】\n")
	if r.AIAnalysis.Total > 0 {
		b.WriteString(fmt.Sprintf("  분석 완료: %s건\n\n", commaInt(r.AIAnalysis.Total)))
	} else {
		b.WriteString("  분석 완료: 0건 (해당 시간대 미실행)\n\n")
	}

	// 종합 평가
	b.WriteString("【종합 평가】\n")
	if r.Summary.Errors > 0 {
		b.WriteString(fmt.Sprintf("  🔴 ERROR %d건 발생 — 즉시 확인 필요\n", r.Summary.Errors))
	} else if r.APIFailures.Total > 0 {
		b.WriteString(fmt.Sprintf("  ✅ 정상 운영 중. API 실패 %d건 발생했으나 재시도 복구 완료.", r.APIFailures.Total))
		ks := r.KISScheduler
		if len(ks.Completed) > 0 {
			b.WriteString(fmt.Sprintf(" KIS %d배치 완료.", len(ks.Completed)))
		}
		if r.News.Total > 0 {
			b.WriteString(" 뉴스 수집 정상.")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("  ✅ 전체 정상 — API 실패 0건, ERROR 0건.")
		ks := r.KISScheduler
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

// inTimeWindow 은 "HH:MM" 기준 2분 윈도우 체크
func inTimeWindow(target string, now time.Time) bool {
	h, m := 0, 0
	fmt.Sscanf(target, "%d:%d", &h, &m)
	targetTime := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
	diff := now.Sub(targetTime)
	return diff >= 0 && diff < 2*time.Minute
}

// inMinuteWindow 은 특정 분 기준 2분 윈도우 체크
func inMinuteWindow(minute int, now time.Time) bool {
	diff := now.Minute() - minute
	return diff >= 0 && diff < 2
}

// --- 서브프로세스 실행 ---

func runSubprocess(bin string, args []string) {
	import_exec_command(bin, args, false)
}

func runSubprocessOutput(bin string, args []string) string {
	return import_exec_command_output(bin, args)
}

// --- PID 관리 ---

func writePID(path string) {
	os.MkdirAll("test_logs", 0755)
	os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func isSchedulerRunning(path string) bool {
	pid, err := readPID(path)
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func schedulerStatus() {
	pidFile := "test_logs/scheduler.pid"
	if isSchedulerRunning(pidFile) {
		pid, _ := readPID(pidFile)
		fmt.Printf("[scheduler] 실행 중 (PID: %d)\n", pid)
	} else {
		fmt.Println("[scheduler] 실행 중이 아닙니다.")
	}
}

func schedulerStop() {
	pidFile := "test_logs/scheduler.pid"
	pid, err := readPID(pidFile)
	if err != nil {
		fmt.Println("[scheduler] PID 파일을 찾을 수 없습니다.")
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("[scheduler] 프로세스를 찾을 수 없습니다.")
		return
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("[scheduler] 종료 시그널 전송 실패: %v\n", err)
		return
	}
	fmt.Printf("[scheduler] PID %d에 종료 시그널 전송 완료\n", pid)
}
