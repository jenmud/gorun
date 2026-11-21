package main

import (
	"io"
	"log"
	"os"

	"github.com/jenmud/gorun"
)

func main() {
	gorun.SetLogOutput(io.Discard) // task logs go to stdout by default
	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.Servers = append(cfg.Servers, gorun.Server{
		Name:      "web-3",
		Hostname:  "10.0.0.3",
		SSH:       gorun.SSH{User: "deploy"},
		TaskNames: []string{"@rollout"},
	})
	if err := cfg.Encode(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
