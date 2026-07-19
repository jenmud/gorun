package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	//ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	//defer cancel()

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

	names := []string{}
	items, err := cfg.Pipeline()
	if err != nil {
		panic(err)
	}

	for _, task := range items {
		names = append(names, task.Name)
	}

	fmt.Printf("%s\n\n", strings.Join(names, " -> "))

	//if err := cfg.Run(ctx); err != nil {
	//	panic(err)
	//}

	sTasks, err := cfg.ServersPipeline()
	for s, tasks := range sTasks {
		names := make([]string, len(tasks))
		for i, task := range tasks {
			names[i] = task.Name
		}
		fmt.Printf("%s: %s\n", s, strings.Join(names, " -> "))
	}
}
