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

	graph, err := NewGraph(cfg.Tasks...)
	if err != nil {
		panic(err)
	}

	names := []string{}
	for _, n := range graph.Nodes() {
		name := fmt.Sprintf("%s (in: %d, out: %d)", n.Task.Name, len(n.Inbound), len(n.Outbound))
		names = append(names, name)
	}

	fmt.Printf("%s\n", strings.Join(names, " -> "))
}
