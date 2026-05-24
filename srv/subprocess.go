package srv

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// import_exec_command 는 서브프로세스를 실행한다.
func import_exec_command(bin string, args []string, wait bool) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Printf("[subprocess] 실행 실패: %v\n", err)
		return
	}

	if wait || true {
		if err := cmd.Wait(); err != nil {
			fmt.Printf("[subprocess] 완료 오류: %v\n", err)
		}
	}
}

// import_exec_command_output 는 서브프로세스를 실행하고 출력을 반환한다.
func import_exec_command_output(bin string, args []string) string {
	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr

	// 10분 타임아웃
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
			fmt.Printf("[subprocess] 실행 오류: %v\n", cmdErr)
			return string(out)
		}
		return string(out)
	case <-time.After(10 * time.Minute):
		cmd.Process.Kill()
		fmt.Println("[subprocess] 타임아웃 (10분)")
		return ""
	}
}
