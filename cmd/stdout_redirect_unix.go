//go:build linux || darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"

	"gorun"
)

// redirectStdoutToLog sends output written to stdout before main() runs to the
// log file. The OTELC build initializes its SDK, and logs that initialization,
// in a package init, which is too early to change the Go logger. The returned
// function points os.Stdout at the terminal again while leaving file
// descriptor 1 on the log file, so libraries that captured os.Stdout during
// init keep writing their logs there.
func redirectStdoutToLog() func() {
	f, err := gorun.OpenLogFile(gorun.LogFilePath())
	if err != nil {
		return func() {}
	}
	defer func() { _ = f.Close() }()

	saved, err := unix.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return func() {}
	}

	if err := unix.Dup2(int(f.Fd()), int(os.Stdout.Fd())); err != nil {
		_ = unix.Close(saved)
		return func() {}
	}

	return func() {
		os.Stdout = os.NewFile(uintptr(saved), "stdout")
	}
}
