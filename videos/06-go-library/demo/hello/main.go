package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"

	"github.com/jenmud/gorun"
)

func main() {
	gorun.SetLogOutput(io.Discard) // task logs go to stdout by default
	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.ResolvePaths(filepath.Dir("Gorunfile"))

	if err := cfg.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
	fmt.Println("all tasks passed")
}
