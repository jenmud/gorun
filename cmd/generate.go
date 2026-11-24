package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/envvars"
	"github.com/jenmud/gorun/internal/llm"
)

// generate prints tasks a language model writes for request, for -generate. The tasks in
// filename, when given, are offered to the model as dependencies. The model runs in-process
// unless GORUN_LLM_URL names a model server.
func generate(ctx context.Context, request, filename string, verbose bool) error {
	var cfg *gorun.Config
	if filename != "" {
		cfg = &gorun.Config{}
		if err := cfg.LoadFile(filename); err != nil {
			return err
		}
	}

	a := gorun.Assistant{Model: envvars.Get("GORUN_LLM_MODEL"), APIKey: envvars.Get("GORUN_LLM_API_KEY")}
	// progress goes to stderr, so the tasks on stdout can go straight to a file
	if a.URL = envvars.Get("GORUN_LLM_URL"); a.URL != "" {
		fmt.Fprintf(os.Stderr, "asking %s at %s\n", cmp.Or(a.Model, gorun.DefaultAssistantModel), a.URL)
	} else {
		dir, err := llmDir()
		if err != nil {
			return err
		}
		engine := &llm.Engine{Dir: dir, Model: a.Model, Progress: os.Stderr, Verbose: verbose}
		defer engine.Close()
		a.Engine = engine
		name := "Qwen2.5 Coder 3B"
		if a.Model != "" {
			name = filepath.Base(a.Model)
		}
		fmt.Fprintf(os.Stderr, "asking %s, built in\n", name)
	}

	text, err := a.GenerateTasks(ctx, request, cfg)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

// llmDir is where the built-in model and llama.cpp are kept, in gorun's cache folder.
func llmDir() (string, error) {
	if dir := envvars.Get("GORUN_CACHE_DIR"); dir != "" {
		return filepath.Join(dir, "llm"), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory: set GORUN_CACHE_DIR: %w", err)
	}
	return filepath.Join(dir, "gorun", "llm"), nil
}
