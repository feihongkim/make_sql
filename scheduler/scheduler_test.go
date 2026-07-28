package scheduler

import (
	"sync"
	"testing"
	"time"
)

// 작업이 패닉해도 스케줄러 프로세스가 죽지 않고, 알림이 나가야 한다.
// 이 복구가 없으면 로그 파싱 오류 하나로 전체 스케줄러가 내려간다.
func TestDispatchRecoversFromTaskPanic(t *testing.T) {
	origExec, origNotify := execTask, notify
	defer func() { execTask, notify = origExec, origNotify }()

	var mu sync.Mutex
	var notified string
	done := make(chan struct{})

	execTask = func(s *Scheduler, task Task) {
		defer close(done)
		panic("의도적 패닉")
	}
	notify = func(msg string) error {
		mu.Lock()
		defer mu.Unlock()
		notified = msg
		return nil
	}

	s := newScheduler()
	task := Task{Label: "panic_task", Commands: []string{"log-analyze"}}

	// 복구되지 않으면 이 시점에서 테스트 프로세스가 통째로 죽는다.
	s.dispatch(task, time.Now().In(loc))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("작업이 실행되지 않았다")
	}

	// 패닉 복구와 알림은 defer 에서 일어나므로 잠깐 기다린다.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := notified
		mu.Unlock()
		if got != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("패닉 알림이 전송되지 않았다")
}

// 알림 전송이 실패해도 복구 자체는 성공해야 한다.
func TestRecoverTaskSurvivesNotifyFailure(t *testing.T) {
	origNotify := notify
	defer func() { notify = origNotify }()
	notify = func(string) error { return errTest }

	func() {
		defer recoverTask(Task{Label: "notify_fail"})
		panic("boom")
	}()
	// 여기까지 왔다는 것은 패닉이 복구됐다는 뜻이다.
}

var errTest = &testError{"전송 실패"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }

func TestInTimeWindow(t *testing.T) {
	base := time.Date(2026, 7, 28, 8, 0, 0, 0, loc)
	cases := []struct {
		name   string
		target string
		now    time.Time
		want   bool
	}{
		{"정각", "08:00", base, true},
		{"1분 후는 윈도우 안", "08:00", base.Add(time.Minute), true},
		{"2분 후는 윈도우 밖", "08:00", base.Add(2 * time.Minute), false},
		{"1분 전은 아직", "08:00", base.Add(-time.Minute), false},
	}
	for _, c := range cases {
		if got := inTimeWindow(c.target, c.now); got != c.want {
			t.Errorf("%s: inTimeWindow(%s, %s) = %v, want %v",
				c.name, c.target, c.now.Format("15:04"), got, c.want)
		}
	}
}

func TestShouldRunSkipsCompleted(t *testing.T) {
	s := newScheduler()
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, loc)
	s.today = now.Format("20060102")
	task := Task{Label: "security_check_morning", Time: "08:00", Commands: []string{"security-check"}}

	if !s.shouldRun(task, now) {
		t.Fatal("실행 시각인데 실행되지 않는다")
	}
	s.completed[s.completedKey(task, now)] = now
	if s.shouldRun(task, now) {
		t.Fatal("이미 완료한 작업이 다시 실행된다")
	}
}
