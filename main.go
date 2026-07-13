package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

	fmt.Println()

	fmt.Println()

	pipeline, err := cfg.Pipeline()
	if err != nil {
		panic(err)
	}

	names := []string{}
	for _, t := range pipeline {
		names = append(names, t.Name)
	}
	fmt.Printf("%s\n", strings.Join(names, " -> "))
}
