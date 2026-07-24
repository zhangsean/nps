//go:build !windows

package restart

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func prepareDetachedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func scheduleDirectExec(payload helperPayload) bool {
	go func() {
		time.Sleep(time.Second)
		arguments := append([]string{payload.Executable}, payload.Arguments...)
		if err := syscall.Exec(payload.Executable, arguments, os.Environ()); err != nil {
			fmt.Fprintf(os.Stderr, "restart current process: %v\n", err)
			clearPending()
		}
	}()
	return true
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find parent process %d: %w", pid, err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err = process.Signal(syscall.Signal(0))
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("check parent process %d: %w", pid, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for parent process %d", pid)
}
