package main

import "os"

func init() {
	for _, a := range os.Args {
		if a == "-version" || a == "--version" {
			_ = os.Setenv("OTEL_LOG_LEVEL", "error")
			_ = os.Setenv("OTEL_SDK_DISABLED", "true")
		}
	}
}
