package gorun

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"go.opentelemetry.io/otel/attribute"
)

// Assistant writes Gorunfile tasks from a plain description, using a language model: Engine
// when it is set, or else one behind an OpenAI-compatible chat API, such as Ollama, llama.cpp's
// llama-server, LM Studio or a hosted service.
type Assistant struct {
	Engine Engine // a model run in-process; when nil, the API at URL is asked
	URL    string // base URL, such as http://localhost:11434/v1
	Model  string
	APIKey string // sent as a bearer token when set
	// Attempts is how many times to ask before giving up on tasks that don't load. Zero means 3.
	Attempts int
	Client   *http.Client
}

// Engine is a language model that answers a conversation.
type Engine interface {
	Chat(ctx context.Context, messages []ChatMessage) (string, error)
}

// ChatMessage is one turn of a conversation with a model. Role is system, user or assistant.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Assistant defaults, for a local Ollama.
const (
	DefaultAssistantURL   = "http://localhost:11434/v1"
	DefaultAssistantModel = "qwen2.5-coder:3b"
)

// assistantPrompt teaches the model the subset of a Gorunfile a task needs.
const assistantPrompt = `You write tasks for gorun, a tool that runs shell commands on servers over SSH.
Reply with TOML only: one or more [tasks.<name>] tables, no prose and no markdown fences.
Task names are short, lowercase, with dashes. Keys a task may use:

  description = "one line summary"         # always set this
  cmd = "shell command"                     # required; use '''multi-line''' strings for scripts
  sudo = true                               # runs cmd, check and verify as root; never write sudo in them
  check = "command"                         # exit 0 means already done, so the task is skipped
  verify = "command"                        # runs after cmd; non-zero fails the task
  depends_on = ["other-task"]               # only names of tasks that exist
  timeout = "5m"
  retries = 2
  rerun = "safe"                            # or "unsafe" when running it twice does harm
  envvars = { KEY = "value" }

Every cmd, check and verify is a real shell command, never a description or a placeholder;
leave check or verify out when you don't know one. Prefer idempotent commands. Example:

[tasks.nginx]
description = "Install nginx and keep it running"
check = "dpkg -s nginx >/dev/null 2>&1 && systemctl is-active --quiet nginx"
cmd = "apt-get install -y nginx && systemctl enable --now nginx"
verify = "systemctl is-active nginx"
sudo = true
rerun = "safe"`

// GenerateTasks asks the model for tasks that do what request describes, and returns them as
// Gorunfile TOML. The tasks in cfg, when given, are offered as dependencies. The reply is
// loaded and validated, and the model is asked again with the error until it loads.
func (a Assistant) GenerateTasks(ctx context.Context, request string, cfg *Config) (string, error) {
	ctx, span := tracer.Start(ctx, "gorun.generate")
	defer span.End()
	span.SetAttributes(attribute.String("gorun.assistant.model", a.model()))

	messages := []ChatMessage{
		{Role: "system", Content: assistantPrompt + existingTasks(cfg, request)},
		// small models follow the last message best, so the format is repeated here
		{Role: "user", Content: request + "\n\nReply with gorun [tasks.<name>] TOML only."},
	}
	var err error
	var reply string
	for range cmp.Or(a.Attempts, 3) {
		reply, err = a.chat(ctx, messages)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		text := stripFences(reply)
		if err = checkTasks(text, cfg); err == nil {
			return text + "\n", nil
		}
		messages = append(messages,
			ChatMessage{Role: "assistant", Content: reply},
			ChatMessage{Role: "user", Content: "That doesn't load: " + err.Error() + ". Reply with the corrected TOML only."},
		)
	}
	span.RecordError(err)
	return "", fmt.Errorf("generate tasks: %w; the model's last reply:\n%s", err, reply)
}

func (a Assistant) model() string { return cmp.Or(a.Model, DefaultAssistantModel) }

// maxExistingTasks caps the tasks listed in the prompt: a small model's context is a few
// thousand tokens, and a long list pushes the instructions out of it.
const maxExistingTasks = 30

// existingTasks lists the cfg tasks most like request, so the model can depend on them.
func existingTasks(cfg *Config, request string) string {
	if cfg == nil || len(cfg.Tasks) == 0 {
		return ""
	}
	words := strings.Fields(strings.ToLower(request))
	score := func(name string) int {
		text := strings.ToLower(name + " " + cfg.Tasks[name].Description)
		n := 0
		for _, w := range words {
			if len(w) > 2 && strings.Contains(text, w) {
				n++
			}
		}
		return n
	}
	names := sortedKeys(cfg.Tasks)
	slices.SortStableFunc(names, func(a, b string) int { return score(b) - score(a) })
	names = names[:min(len(names), maxExistingTasks)]
	slices.Sort(names)

	var b strings.Builder
	b.WriteString("\n\nTasks that already exist (depend on them, don't redefine them):\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- %s: %s\n", name, cfg.Tasks[name].Description)
	}
	return b.String()
}

var fence = regexp.MustCompile("(?s)```[a-zA-Z]*\\n(.*?)```")

// stripFences takes the TOML out of a markdown code block, which small models add anyway.
func stripFences(reply string) string {
	if m := fence.FindStringSubmatch(reply); m != nil {
		reply = m[1]
	}
	return strings.TrimSpace(reply)
}

// checkTasks reports why text isn't a set of new, valid tasks.
func checkTasks(text string, cfg *Config) error {
	var file struct {
		Tasks map[string]Task `toml:"tasks"`
	}
	meta, err := toml.Decode(text, &file)
	if err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return fmt.Errorf("unknown key %s", keys[0])
	}
	if len(file.Tasks) == 0 {
		return errors.New("no [tasks.<name>] tables")
	}
	var existing map[string]Task
	if cfg != nil {
		existing = cfg.Tasks
	}
	for _, name := range sortedKeys(file.Tasks) {
		task := file.Tasks[name]
		task.Name = name
		if _, ok := existing[name]; ok {
			return fmt.Errorf("task %s already exists", name)
		}
		if err := task.validate(); err != nil {
			return err
		}
		for _, dep := range task.DependsOn {
			_, isNew := file.Tasks[dep]
			if _, ok := existing[dep]; !ok && !isNew {
				return fmt.Errorf("task %s depends on %s, which doesn't exist", name, dep)
			}
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// chat sends messages to the engine, or the chat completions endpoint, and returns the reply.
func (a Assistant) chat(ctx context.Context, messages []ChatMessage) (string, error) {
	if a.Engine != nil {
		return a.Engine.Chat(ctx, messages)
	}
	body, err := json.Marshal(map[string]any{"model": a.model(), "messages": messages, "temperature": 0.2})
	if err != nil {
		return "", err
	}
	url := strings.TrimSuffix(cmp.Or(a.URL, DefaultAssistantURL), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute} // a small model on a CPU is slow
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ask %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ask %s: %s: %s", url, resp.Status, bytes.TrimSpace(data))
	}
	var reply struct {
		Choices []struct {
			Message ChatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		return "", fmt.Errorf("read reply: %w", err)
	}
	if len(reply.Choices) == 0 {
		return "", errors.New("the model sent no reply")
	}
	return reply.Choices[0].Message.Content, nil
}
