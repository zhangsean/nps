//go:build windows

package restart

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func prepareDetachedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func scheduleDirectExec(payload helperPayload) bool {
	return false
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find parent process %d: %w", pid, err)
	}
	done := make(chan error, 1)
	go func() {
		_, waitErr := process.Wait()
		done <- waitErr
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("wait for parent process %d: %w", pid, err)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for parent process %d", pid)
	}
}
