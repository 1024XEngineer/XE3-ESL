//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const linuxWatchdogScript = `set -eu
terminate() {
	trap - HUP TERM
	/bin/kill -KILL -- "-$$"
}
trap terminate HUP TERM
"$@" &
child=$!
set +e
wait "$child"
status=$?
set -e
trap - HUP TERM
exit "$status"
`

func configureCommandProcess(command *exec.Cmd) {
	target := append([]string(nil), command.Args...)
	command.Path = "/bin/sh"
	command.Args = append([]string{
		"sh", "-c", linuxWatchdogScript, "staging-broker-watchdog",
	}, target...)
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}
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
