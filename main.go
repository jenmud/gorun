package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	flag.Parse()
	filename := flag.Arg(0)

	slog.Info("parsing gorun file", slog.String("filename", filename))

	cfg := Config{}

	r, err := os.Open(filename)
	if err != nil {
		panic(err)
	}

	if err := cfg.Load(r); err != nil {
		panic(err)
	}

	slog.Info("parsed config file")
	slog.Info("config parsed", slog.Any("cfg", cfg))

	fmt.Println()
	for _, s := range cfg.UniqueServers() {
		fmt.Printf("\t- %s\n", s.Hostname)
	}
}
