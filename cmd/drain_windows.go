//go:build windows

package main

// drainTerminalInput does nothing on Windows.
func drainTerminalInput() {}
