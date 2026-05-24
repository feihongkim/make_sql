package srv

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

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
			Label:    "morning_briefing",
			Time:     "08:00",
			Commands: []string{"morning-briefing"},
		},
		{
			Label:    "todo_copy",
			Time:     "08:00",
			Commands: []string{"todo-copy"},
		},
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
		case "morning-briefing":
			s.runMorningBriefing()
		case "todo-copy":
			s.runTodoCopy()
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

func (s *Scheduler) runMorningBriefing() {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("[scheduler] 실행 경로 확인 실패: %v\n", err)
		return
	}

	// claude를 사용해서 일정 + 메일 + Notion 확인 후 텔레그램 전송
	// 로컬 MCP 서버 도구명 사용: cal_list_events, gmail_search, notion_get_blocks
	prompt := `다음 작업을 수행해줘:
1) cal_list_events 도구로 오늘 일정을 확인해줘
2) gmail_search 도구로 최근 24시간 동안 받은 메일을 확인해줘 (query: "newer_than:1d")
3) notion_get_blocks 도구로 ToDoList 페이지(block_id: 10e6b904-d9ee-8063-bed0-f14c593a810d)의 하위 블록을 가져오고, 가장 마지막 child_page의 block_id로 다시 notion_get_blocks를 호출해서 미완료 to_do 항목을 확인해줘
4) 결과를 아래 형식으로 텔레그램 chat_id 7723743534에 전송해줘:

📅 오늘의 일정 (날짜)
- 일정 목록 (없으면 "일정 없음")

📬 최근 24시간 메일 (건수)
- 보낸사람 / 제목 (전체)

📋 Notion ToDoList (미완료)
- 미완료 항목 목록
- 진행률 표시
`

	runSubprocess(self, []string{"claude", "MakeSQL", prompt})
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

	prompt := fmt.Sprintf(`다음 JSON은 nginx 접근 로그 보안 분석 결과야 (최근 2시간).
feivyblog(블로그), moodle(LMS), unknown(서버 판별 불가) 세 서버에 대해
보안 위협, 봇/스캐너, 무차별 대입, 정상 트래픽 현황을 요약해서
텔레그램 chat_id 7723743534로 리포트 보내줘.
위협이 없으면 "이상 없음"으로 간단히, 위협이 있으면 IP와 공격 유형을 명시해줘.

%s`, output)

	runSubprocess(self, []string{"claude", "MakeSQL", prompt})
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

func (s *Scheduler) runTodoCopy() {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("[scheduler] 실행 경로 확인 실패: %v\n", err)
		return
	}

	today := time.Now().In(loc).Format("060102") // YYMMDD

	prompt := fmt.Sprintf(`다음 작업을 수행해줘:
1) notion_get_blocks 도구로 ToDoList 페이지(block_id: 10e6b904-d9ee-8063-bed0-f14c593a810d)의 하위 블록을 가져와서 child_page 목록을 확인해줘
2) 가장 마지막 child_page의 block_id로 notion_get_blocks를 호출해서 모든 to_do 항목을 가져와줘
3) 오늘 날짜(%s)와 같은 제목의 child_page가 이미 있으면 아무것도 하지 말고 "이미 존재" 라고만 출력해줘
4) 없으면 notion-create-pages 도구로 ToDoList 페이지(page_id: 10e6b904-d9ee-8063-bed0-f14c593a810d) 하위에 제목 "%s"인 새 페이지를 생성하고, 가져온 to_do 항목을 동일한 checked 상태로 모두 복사해줘
`, today, today)

	runSubprocess(self, []string{"claude", "MakeSQL", prompt})
}

func (s *Scheduler) runLogAnalyze(args []string) {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("[scheduler] 실행 경로 확인 실패: %v\n", err)
		return
	}

	// log-analyze 실행
	analyzeArgs := append([]string{"log-analyze"}, args...)
	output := runSubprocessOutput(self, analyzeArgs)

	if output == "" {
		fmt.Println("[scheduler] log-analyze 결과 없음")
		return
	}

	// claude로 분석 결과를 텔레그램에 전송
	prompt := fmt.Sprintf(`다음 JSON은 White 서버 LOG 분석 결과야.
모듈별 요약, API 실패, ERROR, KIS 완료 현황, 뉴스/AI 상태를 정리해서
텔레그램 chat_id 7723743534로 요약 리포트를 전송해줘.

%s`, output)

	runSubprocess(self, []string{"claude", "MakeSQL", prompt})
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
