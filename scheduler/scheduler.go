package scheduler

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"MakeSQL/console"
)

var loc *time.Location

func init() {
	loc, _ = time.LoadLocation("Asia/Seoul")
}

// Task 는 스케줄 단위 작업을 정의한다.
type Task struct {
	Label     string
	Time      string               // "HH:MM" 고정 실행 시각
	Every     int                  // 반복 간격 (시간 단위, 0이면 Time 기반)
	AtMinute  int                  // Every 사용 시 실행 분
	Commands  []string             // 실행할 서브커맨드
	Condition func(time.Time) bool // 실행 조건 (nil이면 항상)
}

// BuildSchedule 은 스케줄 목록을 구성한다.
func BuildSchedule() []Task {
	return []Task{
		{Label: "log_analyze", Every: 3, AtMinute: 40, Commands: []string{"log-analyze", "white", "3"}},
		{Label: "nginx_analyze", Every: 3, AtMinute: 10, Commands: []string{"nginx-analyze"}},
		{Label: "security_check", Every: 3, AtMinute: 30, Commands: []string{"security-check"}},
		{Label: "surge_sync", Time: "00:00", Commands: []string{"surge-sync"}},
		{Label: "blog_sync", Every: 1, AtMinute: 17, Commands: []string{"blog-sync"}},
		{Label: "temp_check", Every: 3, AtMinute: 50, Commands: []string{"temp-check"}},
		{Label: "tg_monitor", Every: 1, AtMinute: 5, Commands: []string{"tg-monitor"}},
	}
}

// Scheduler 는 스케줄러 상태를 관리한다.
type Scheduler struct {
	tasks     []Task
	completed map[string]time.Time
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
	if isSchedulerRunning(s.pidFile) {
		console.Log("[scheduler] 이미 실행 중입니다.")
		os.Exit(1)
	}
	writePID(s.pidFile)
	defer os.Remove(s.pidFile)

	console.Log("[scheduler] 스케줄러 시작")
	console.Log("[scheduler] 등록된 태스크:")
	for _, t := range s.tasks {
		if t.Every > 0 {
			console.Log("  - %s: 매 %d시간 %d분에 실행", t.Label, t.Every, t.AtMinute)
		} else {
			console.Log("  - %s: 매일 %s 실행", t.Label, t.Time)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		console.Log("[scheduler] 종료 시그널 수신")
		close(s.stopCh)
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.tick()

	for {
		select {
		case <-s.stopCh:
			console.Log("[scheduler] 스케줄러 종료")
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	now := time.Now().In(loc)
	day := now.Format("20060102")
	if day != s.today {
		s.completed = make(map[string]time.Time)
		s.today = day
		console.Log("[scheduler] 날짜 변경: %s", day)
	}
	for _, task := range s.tasks {
		if s.shouldRun(task, now) {
			s.dispatch(task, now)
		}
	}
}

func (s *Scheduler) shouldRun(task Task, now time.Time) bool {
	key := s.completedKey(task, now)
	if _, done := s.completed[key]; done {
		return false
	}
	if task.Condition != nil && !task.Condition(now) {
		return false
	}
	if task.Every > 0 {
		if now.Hour()%task.Every != 0 {
			return false
		}
		return inMinuteWindow(task.AtMinute, now)
	}
	return inTimeWindow(task.Time, now)
}

func (s *Scheduler) completedKey(task Task, now time.Time) string {
	if task.Every > 0 {
		return fmt.Sprintf("%s:%s:%02d", s.today, task.Label, now.Hour())
	}
	return s.today + ":" + task.Label
}

func (s *Scheduler) dispatch(task Task, now time.Time) {
	key := s.completedKey(task, now)
	s.completed[key] = now
	console.Log("[scheduler] [%s] %s 실행 시작", now.Format("15:04:05"), task.Label)
	go func() {
		switch task.Commands[0] {
		case "nginx-analyze":
			s.runNginxAnalyze()
		case "log-analyze":
			s.runLogAnalyze(task.Commands[1:])
		case "security-check":
			s.runSecurityCheck()
		case "surge-sync":
			s.runSurgeSync()
		case "blog-sync":
			s.runBlogSync()
		case "temp-check":
			s.runTempCheck()
		case "tg-monitor":
			s.runTgMonitor()
		default:
			console.LogError("[scheduler] 알 수 없는 명령: %s", task.Commands[0])
		}
		console.Log("[scheduler] [%s] %s 완료", time.Now().In(loc).Format("15:04:05"), task.Label)
	}()
}

// inTimeWindow 는 "HH:MM" 기준 2분 윈도우 체크
func inTimeWindow(target string, now time.Time) bool {
	h, m := 0, 0
	fmt.Sscanf(target, "%d:%d", &h, &m)
	targetTime := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
	diff := now.Sub(targetTime)
	return diff >= 0 && diff < 2*time.Minute
}

// inMinuteWindow 는 특정 분 기준 2분 윈도우 체크
func inMinuteWindow(minute int, now time.Time) bool {
	diff := now.Minute() - minute
	return diff >= 0 && diff < 2
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
		console.Log("[scheduler] 실행 중 (PID: %d)", pid)
	} else {
		console.Log("[scheduler] 실행 중이 아닙니다.")
	}
}

func schedulerStop() {
	pidFile := "test_logs/scheduler.pid"
	pid, err := readPID(pidFile)
	if err != nil {
		console.LogError("[scheduler] PID 파일을 찾을 수 없습니다.")
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		console.LogError("[scheduler] 프로세스를 찾을 수 없습니다.")
		return
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		console.LogError("[scheduler] 종료 시그널 전송 실패: %v", err)
		return
	}
	console.Log("[scheduler] PID %d에 종료 시그널 전송 완료", pid)
}
