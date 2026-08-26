//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureCommandProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	command.WaitDelay = 5 * time.Second
}

func terminationSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP, syscall.SIGTERM}
}
