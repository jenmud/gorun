//go:build !linux && !darwin

package main

// redirectStdoutToLog is a no-op where stdout redirection is not implemented.
// The instrumented build is only released for Linux.
func redirectStdoutToLog() func() { return func() {} }
