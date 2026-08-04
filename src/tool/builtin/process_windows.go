//go:build windows

package builtin

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

func configureCommandForCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return killProcessTree(cmd)
	}
	cmd.WaitDelay = bashCancelWaitDelay
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return os.ErrProcessDone
	}

	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil {
			if errors.Is(killErr, os.ErrProcessDone) {
				return os.ErrProcessDone
			}
			return errors.Join(err, killErr)
		}
	}
	return nil
}
