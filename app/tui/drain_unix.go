//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func drainTerminalInput() {
	f := os.Stdin
	if !term.IsTerminal(int(f.Fd())) {
		return
	}
	fd := int(f.Fd())
	buf := make([]byte, 4096)
	timeout := 200
	for {
		pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pfds, timeout)
		if err != nil || n == 0 {
			break
		}
		if pfds[0].Revents&unix.POLLIN == 0 {
			break
		}
		if _, err := f.Read(buf); err != nil {
			break
		}
		timeout = 0
	}
}
