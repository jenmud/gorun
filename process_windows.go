//go:build windows

package gorun

import "os/exec"

// configureCommand bounds how long Wait can block on inherited output pipes
// after a command is canceled. Windows has no process groups here, so the
// command's own process is killed by exec.CommandContext.
func configureCommand(cmd *exec.Cmd) {
	cmd.WaitDelay = taskKillGrace
}
