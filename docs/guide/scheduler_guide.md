# Go 자체 스케줄러 구현 가이드

시스템 crontab 대신 Go 프로세스 내부에서 24시간 스케줄링을 직접 관리하는 패턴. 의존성 체크, 조건부 실행, 프로세스 추적 등 crontab으로 구현하기 어려운 기능을 제공한다.

## crontab 대비 장점

| 기능 | crontab | 자체 스케줄러 |
|------|---------|-------------|
| 태스크 의존성 (A 끝나야 B 실행) | 불가 | DependsOn |
| 동적 스케줄 사이클 (15:30 기준 리셋) | 어려움 | scheduleDay() |
| 실행 중인 프로세스 추적 | 별도 구현 필요 | running map |
| Graceful shutdown | 불가 | SIGTERM + waitAllRunning |
| 중복 실행 방지 | flock 등 별도 | PID 파일 + completed map |
| 조건부 실행 (요일, 스케줄일 기준) | 제한적 | Condition 함수 |

## 아키텍처 개요

```
┌─────────────────────────────────────────────┐
│ Scheduler (상주 프로세스)                      │
│                                             │
│  30초 Ticker ──→ tick()                      │
│                   ├─ 스케줄일 리셋 체크         │
│                   ├─ 완료 프로세스 수확         │
│                   └─ 각 Task에 shouldRun() 체크│
│                       └─ dispatch() → spawn() │
│                           └─ exec.Command     │
│                              (독립 OS 프로세스)  │
└─────────────────────────────────────────────┘
```

## 파일 구조

```
cmd/scheduler/
├── engine.go    # 스케줄러 메인 루프, tick, shouldRun, dispatch, spawn
├── tasks.go     # Task/Phase 구조체, BuildSchedule(), 조건 함수
└── control.go   # PID 관리, status/stop 명령
```

## 1단계: 데이터 구조 정의 (tasks.go)

```go
package scheduler

import "time"

// Task는 스케줄 단위 작업을 정의한다.
type Task struct {
    Time      string               // 실행 시각 "HH:MM"
    Commands  []string             // 실행할 서브커맨드 목록
    Parallel  bool                 // true면 Commands를 동시 실행, false면 순차
    DependsOn string               // 이 label이 완료되어야 실행
    Condition func(time.Time) bool // 실행 조건 (nil이면 항상 실행)
    Label     string               // 태스크 고유 식별자
}

// Phase는 관련 태스크를 그룹핑한다.
type Phase struct {
    Name  string
    Tasks []Task
}
```

### Task 필드 설명

| 필드 | 설명 | 예시 |
|------|------|------|
| Time | 실행 예정 시각 (KST) | `"15:45"` |
| Commands | 서브프로세스로 실행할 명령 | `["collect DM.BP", "collect DM.PA"]` |
| Parallel | Commands 동시 실행 여부 | `true` → 3개 병렬 |
| DependsOn | 의존 태스크 label | `"token_clean"` |
| Condition | 실행 조건 함수 | `isWeekday`, `isMonday` |
| Label | 완료 추적용 고유키 | `"kr_main"` |

## 2단계: 스케줄 정의 (tasks.go)

```go
func BuildSchedule() []Phase {
    return []Phase{
        {Name: "Phase 1: 준비", Tasks: []Task{
            {Time: "09:00", Commands: []string{"prepare"},
                Label: "prepare"},
        }},

        {Name: "Phase 2: 수집", Tasks: []Task{
            // 병렬 실행, prepare 완료 후
            {Time: "09:10", Commands: []string{"collect A", "collect B"},
                Parallel: true, Label: "collect_main",
                DependsOn: "prepare", Condition: isWeekday},
        }},

        {Name: "Phase 3: 분석", Tasks: []Task{
            // collect_main 완료 후 실행
            {Time: "12:00", Commands: []string{"analyze"},
                Label: "analyze",
                DependsOn: "collect_main", Condition: isWeekday},
        }},
    }
}

// 조건 함수들
func isWeekday(t time.Time) bool {
    d := t.Weekday()
    return d >= time.Monday && d <= time.Friday
}

func isMonday(t time.Time) bool {
    return t.Weekday() == time.Monday
}

func notSunday(t time.Time) bool {
    return t.Weekday() != time.Sunday
}
```

## 3단계: 스케줄러 엔진 (engine.go)

### 핵심 구조체

```go
type Scheduler struct {
    phases    []Phase
    running   map[string][]*exec.Cmd  // label → 실행 중인 프로세스들
    completed map[string]time.Time    // "YYYYMMDD:label" → 완료 시각
    mu        sync.Mutex
    stopCh    chan struct{}
    today     string                  // 현재 스케줄일 "YYYYMMDD"
}
```

### 메인 루프

```go
func Run() {
    s := &Scheduler{
        phases:    BuildSchedule(),
        running:   make(map[string][]*exec.Cmd),
        completed: make(map[string]time.Time),
        stopCh:    make(chan struct{}),
        today:     scheduleDay(time.Now()),
    }

    // PID 파일로 중복 실행 방지
    WritePID()
    defer RemovePID()

    // SIGTERM graceful shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    go func() {
        <-sigCh
        close(s.stopCh)
    }()

    // 30초마다 tick
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    s.tick() // 초기 체크

    for {
        select {
        case <-s.stopCh:
            s.waitAllRunning() // 실행 중인 작업 완료 대기
            return
        case <-ticker.C:
            s.tick()
        }
    }
}
```

### tick() — 30초마다 실행되는 핵심 로직

```go
func (s *Scheduler) tick() {
    now := time.Now()
    day := scheduleDay(now)

    // 스케줄일 변경 시 completed 리셋
    if day != s.today {
        s.mu.Lock()
        s.completed = make(map[string]time.Time)
        s.today = day
        s.mu.Unlock()
    }

    // 모든 Phase의 모든 Task 순회
    for _, phase := range s.phases {
        for _, task := range phase.Tasks {
            if s.shouldRun(task, now) {
                s.dispatch(task, phase.Name)
            }
        }
    }
}
```

### shouldRun() — 실행 여부 판단 (5단계 체크)

```go
func (s *Scheduler) shouldRun(task Task, now time.Time) bool {
    s.mu.Lock()
    defer s.mu.Unlock()

    key := s.today + ":" + task.Label

    // ① 오늘 이미 완료?
    if _, done := s.completed[key]; done {
        return false
    }

    // ② 이미 실행 중?
    if _, running := s.running[task.Label]; running {
        return false
    }

    // ③ 시간 도달?
    if task.DependsOn != "" {
        // DependsOn 있으면: 예정시간 이후면 OK (지연 대응)
        if !isPastTime(task.Time, now) {
            return false
        }
    } else {
        // DependsOn 없으면: 2분 윈도우
        if !inTimeWindow(task.Time, now) {
            return false
        }
    }

    // ④ 조건 충족? (스케줄일 기준 판단)
    condTime := scheduleDayTime(s.today)
    if task.Condition != nil && !task.Condition(condTime) {
        s.completed[key] = now // 조건 미충족 시 완료 처리 (재시도 방지)
        return false
    }

    // ⑤ 의존 태스크 완료?
    if task.DependsOn != "" {
        depKey := s.today + ":" + task.DependsOn
        if _, done := s.completed[depKey]; !done {
            return false
        }
    }

    return true
}
```

### 시간 체크 함수

```go
// 2분 윈도우: 독립 태스크용 (중복 실행 방지)
func inTimeWindow(target string, now time.Time) bool {
    h, m := 0, 0
    fmt.Sscanf(target, "%d:%d", &h, &m)
    targetTime := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
    diff := now.Sub(targetTime)
    return diff >= 0 && diff < 2*time.Minute
}

// 예정시간 이후: 의존 태스크용 (의존 대상 지연 시에도 실행 가능)
func isPastTime(target string, now time.Time) bool {
    h, m := 0, 0
    fmt.Sscanf(target, "%d:%d", &h, &m)
    targetTime := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
    return !now.Before(targetTime)
}
```

### 왜 2가지 시간 체크가 필요한가

| 상황 | inTimeWindow (2분) | isPastTime (이후) |
|------|-------------------|------------------|
| 독립 태스크 (DependsOn 없음) | ✅ 사용 | ❌ |
| 의존 태스크 (DependsOn 있음) | ❌ | ✅ 사용 |

**문제 시나리오**: `analyze`(12:00)가 `collect_main`에 의존. collect_main이 13:00에 끝남.
- `inTimeWindow`: 12:00~12:02 지남 → 영원히 실행 불가 ❌
- `isPastTime`: 12:00 이후 + 의존 완료 → 13:00에 즉시 실행 ✅

### dispatch() — 프로세스 생성

```go
func (s *Scheduler) dispatch(task Task, phaseName string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    var cmds []*exec.Cmd
    for _, cmdStr := range task.Commands {
        cmd := s.spawn(cmdStr)
        cmds = append(cmds, cmd)
        if !task.Parallel {
            go s.runSequential(task, cmds) // 순차 실행
            return
        }
    }

    s.running[task.Label] = cmds
    go s.monitorParallel(task) // 병렬 완료 감시
}

func (s *Scheduler) spawn(cmdStr string) *exec.Cmd {
    args := strings.Fields(cmdStr)
    self, _ := os.Executable() // 자기 자신의 바이너리 경로
    cmd := exec.Command(self, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.Start()
    return cmd
}
```

### 완료 처리

```go
// 병렬: 모든 프로세스 완료 대기
func (s *Scheduler) monitorParallel(task Task) {
    for _, cmd := range s.running[task.Label] {
        cmd.Wait()
    }
    s.markComplete(task.Label)
}

// 순차: 하나씩 실행 후 대기
func (s *Scheduler) runSequential(task Task, initialCmds []*exec.Cmd) {
    initialCmds[0].Wait()
    for i := 1; i < len(task.Commands); i++ {
        cmd := s.spawn(task.Commands[i])
        cmd.Wait()
    }
    s.markComplete(task.Label)
}

func (s *Scheduler) markComplete(label string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.running, label)
    s.completed[s.today+":"+label] = time.Now()
}
```

## 4단계: 프로세스 관리 (control.go)

```go
const pidFile = "test_logs/scheduler.pid"

// PID 파일 기반 중복 실행 방지
func WritePID() error {
    return os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func RemovePID() { os.Remove(pidFile) }

func IsRunning() bool {
    pid, err := readPID()
    if err != nil { return false }
    return processAlive(pid)
}

// 프로세스 생존 확인 (signal 0 전송)
func processAlive(pid int) bool {
    proc, err := os.FindProcess(pid)
    if err != nil { return false }
    return proc.Signal(syscall.Signal(0)) == nil
}

// 외부에서 스케줄러 중지
func Stop() {
    pid, _ := readPID()
    proc, _ := os.FindProcess(pid)
    proc.Signal(syscall.SIGTERM)
}
```

## 5단계: 스케줄 사이클 설계

### scheduleDay — 사이클 경계 정의

```go
// 15:30 기준으로 스케줄일 결정
// 15:30 이전 → 전날 사이클
// 15:30 이후 → 오늘 사이클
func scheduleDay(t time.Time) string {
    if t.Hour() < 15 || (t.Hour() == 15 && t.Minute() < 30) {
        t = t.AddDate(0, 0, -1)
    }
    return t.Format("20060102")
}
```

이것이 중요한 이유:

```
목요일 15:30 → 스케줄일 = 목요일
  Phase 2: 한국장 수집 (15:45~)
  Phase 4: 아시아장 수집 (17:00~)
  Phase 3: 전략 시그널 (01:00~) ← 금요일 새벽이지만 목요일 사이클
  Phase 5: 클린업 (06:00) ← 금요일 아침이지만 목요일 사이클
  Phase 7: 미국장 수집 (06:30~)
금요일 15:30 → 새 스케줄일 = 금요일 (completed 리셋)
```

### Condition과 스케줄일

Condition 체크 시 `now`(현재 시각)가 아닌 `scheduleDayTime`(스케줄일)을 사용한다.

```go
condTime := scheduleDayTime(s.today) // "20260409" → time.Time(2026-04-09)
task.Condition(condTime)              // isWeekday(수요일) → true
```

**이유**: 금요일 사이클의 토요일 아침 실행을 허용하기 위함.
- 금요일 사이클 → 토요일 06:00에 cleanup 실행
- `now` 사용 시: isWeekday(토요일) = false → 스킵 ❌
- `scheduleDay` 사용 시: isWeekday(금요일) = true → 실행 ✅

## 실행 방법

```bash
# 빌드
go build -o MyApp_Hope .

# 포그라운드 실행
./MyApp_Hope scheduler

# 백그라운드 실행 (로그 파일)
TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
nohup ./MyApp_Hope scheduler >> test_logs/${TIMESTAMP}_scheduler.log 2>&1 &

# 상태 확인
./MyApp_Hope scheduler status

# 중지 (graceful)
./MyApp_Hope scheduler stop
```

## CLI 라우터 연동

```go
// cmd/router.go
func Run(args []string) {
    switch args[1] {
    case "scheduler":
        if len(args) > 2 {
            switch args[2] {
            case "status": scheduler.Status()
            case "stop":   scheduler.Stop()
            }
            return
        }
        scheduler.Run()
    case "collect":
        runCollect(args[2:])
    }
}
```

## 설계 시 고려사항

### 1. Ticker 간격
- 30초 권장. 1분이면 2분 윈도우에서 놓칠 수 있음
- CPU 부하 무시할 수준 (tick당 조건 체크만)

### 2. 윈도우 크기
- 2분 = Ticker 30초 × 4회. 최소 3~4회 체크 기회 보장
- 너무 크면 재시작 시 의도치 않은 재실행

### 3. 재시작 시 주의
- completed 맵 초기화 → 지나간 독립 태스크(2분 윈도우)는 복구 불가
- 의존 태스크는 의존 대상이 completed에 없으므로 실행 안 됨
- 수동 대처 필요 (cleanup → token → collect 수동 실행)

### 4. os.Executable()
- spawn 시 바이너리 경로를 하드코딩하지 않고 동적 감지
- 빌드 바이너리명 변경에 자동 대응 (예: KIS → KIS_Hope)

### 5. 서브프로세스 독립성
- exec.Command로 생성된 프로세스는 완전 독립 OS 프로세스
- 스케줄러가 죽어도 서브프로세스는 계속 실행
- 스케줄러는 cmd.Wait()로 완료만 감시

### 6. Mutex 범위
- shouldRun: Lock으로 completed/running 동시 읽기 보호
- dispatch: Lock으로 running 쓰기 보호
- markComplete: Lock으로 running 삭제 + completed 쓰기
- spawn: Lock 불필요 (프로세스 생성은 thread-safe)
