//go:build unix

package gorun

import (
	"os/exec"
	"syscall"
)

// configureCommand makes sure a timed-out local task stops its whole process
// group, not just the shell gorun started. Shells often fork their children,
// and those children can keep running (and keep the output pipes open) after
// the shell itself is killed.
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}

		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = taskKillGrace
}
