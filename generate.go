package gorun

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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

// ChatTrace receives what a chat is doing while it runs, so a page can show it, as
// net/http/httptrace does for requests. Either field may be nil. Engines read it with
// ChatTraceFrom.
type ChatTrace struct {
	// Status is called with each stage, such as a download's progress or loading the model.
	Status func(text string)
	// Token is called with each piece of the reply as the model writes it. A piece is whole
	// UTF-8. Engines that can't stream don't call it; their reply arrives all at once.
	Token func(piece string)
}

type chatTraceKey struct{}

// WithChatTrace returns ctx carrying t, for the chats run with it.
func WithChatTrace(ctx context.Context, t *ChatTrace) context.Context {
	return context.WithValue(ctx, chatTraceKey{}, t)
}

// ChatTraceFrom returns the trace in ctx. It is never nil, so its methods can always be called.
func ChatTraceFrom(ctx context.Context) *ChatTrace {
	if t, ok := ctx.Value(chatTraceKey{}).(*ChatTrace); ok && t != nil {
		return t
	}
	return &ChatTrace{}
}

// Statusf reports a stage, when anyone is listening.
func (t *ChatTrace) Statusf(format string, args ...any) {
	if t.Status != nil {
		t.Status(fmt.Sprintf(format, args...))
	}
}

// Piece reports a piece of the reply, when anyone is listening.
func (t *ChatTrace) Piece(piece string) {
	if t.Token != nil && piece != "" {
		t.Token(piece)
	}
}

// ChatMessage is one turn of a conversation with a model. Role is system, user or assistant.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Assistant defaults, for a local Ollama.
const (
	DefaultAssistantURL   = "http://localhost:11434/v1"
	DefaultAssistantModel = "qwen3:4b-instruct"
)

// assistantPrompt teaches the model the subset of a Gorunfile a task needs.
const assistantPrompt = `You write tasks for gorun, a tool that runs shell commands on servers over SSH.
Reply with TOML only: one or more [tasks.<name>] tables, no prose and no markdown fences.
` + TaskFormat

// TaskFormat tells a model how to write gorun tasks: the keys a task may use, the rules and
// an example. It is part of the prompt GenerateTasks sends, for prompts of your own.
const TaskFormat = `Task names are short, lowercase, with dashes. Keys a task may use:

  description = "one line summary"         # always set this
  cmd = "shell command"                     # required; use '''multi-line''' strings for scripts
  sudo = true                               # runs cmd, check and verify as root; never write sudo in them
  check = "command"                         # required: exit 0 means already done, so the task is skipped
  verify = "command"                        # required: runs after cmd; non-zero fails the task
  depends_on = ["other-task"]               # only names of tasks that exist
  timeout = "5m"
  retries = 2
  rerun = "safe"                            # or "unsafe" when running it twice does harm
  envvars = { KEY = "value" }
  local = true                              # runs on the machine gorun runs on; see below

A task runs over SSH on the Gorunfile's servers unless it sets local = true, which runs it on
the machine gorun runs on instead. A local task's dependencies must be local too, or they are
skipped there. When the Gorunfile has no servers that run over SSH, set local = true on every task.

A file the task needs (a script, a web page, a config) goes in a copy table, not in cmd. Copies
are written before cmd runs, as the login user, so write to /tmp and move it into place in cmd:

  [[tasks.<name>.copy]]
  dst = "/tmp/index.html"
  content = """<html>...</html>"""         # the file's text
  mode = "0644"

Keep each cmd to a few lines. Never write a program into cmd with python3 -c, node -e or a
heredoc: put it in a copy table's content and run the file. A '''string''' ends at the next '''
and a """string""" at the next """, so never put the same three quotes inside one.

Keep tasks small: each does one step, such as installing one package, writing one config file
or starting one service. Split a bigger job into several small tasks, each depending on the step
before it with depends_on; the last one is the task to run.

Every task has a check and a verify. check exits 0 when the step is done already, so the task is
skipped; verify exits non-zero when cmd didn't work. Every cmd, check and verify is a real shell
command, never a description or a placeholder. Prefer idempotent commands. Example:

[tasks.nginx-install]
description = "Install nginx"
check = "dpkg -s nginx >/dev/null 2>&1"
cmd = "apt-get install -y nginx"
verify = "nginx -v"
sudo = true
rerun = "safe"

[tasks.nginx-running]
description = "Start nginx and start it at boot"
depends_on = ["nginx-install"]
check = "systemctl is-active --quiet nginx && systemctl is-enabled --quiet nginx"
cmd = "systemctl enable --now nginx"
verify = "systemctl is-active nginx"
sudo = true
rerun = "safe"

To run a task or a bundle at set times, add a [[schedule]] entry next to the tasks. gorun -serve
runs it. Keys a schedule may use:

  name = "nightly-backup"                   # required, unique
  description = "one line summary"
  target = "task-name"                      # required: a task, or "@bundle"
  cron = "0 2 * * *"                        # five fields: minute hour day month weekday
  at = "2027-01-15T10:00"                   # instead of cron, to run once
  timezone = "Australia/Sydney"             # IANA zone for cron and at; default UTC
  missed = "run"                            # run once on start-up if a time was missed; default skip

Set exactly one of cron or at. To change a schedule, write its whole [[schedule]] entry again
with the same name.`

// GenerateTasks asks the model for tasks that do what request describes, and returns them as
// Gorunfile TOML. The tasks in cfg, when given, are offered as dependencies. The reply is
// loaded and validated, and the model is asked again with the error until it loads.
func (a Assistant) GenerateTasks(ctx context.Context, request string, cfg *Config) (string, error) {
	ctx, span := tracer.Start(ctx, "gorun.generate")
	defer span.End()
	span.SetAttributes(attribute.String("gorun.assistant.model", a.model()))

	messages := []ChatMessage{
		{Role: "system", Content: assistantPrompt + existingTasks(cfg, request) + LocalNote(cfg)},
		// small models follow the last message best, so the format is repeated here
		{Role: "user", Content: request + "\n\nReply with gorun [tasks.<name>] TOML only."},
	}
	var err error
	var reply string
	for range cmp.Or(a.Attempts, 3) {
		reply, err = a.Chat(ctx, messages)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		var text string
		if text, err = ExtractTasks(reply, cfg); err == nil {
			return text, nil
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

// LocalNote tells a model that cfg has no servers that run over SSH, so the tasks it writes must
// set local = true to run at all. It is empty when cfg has one.
func LocalNote(cfg *Config) string {
	if cfg == nil || slices.ContainsFunc(cfg.Servers, func(s Server) bool { return !s.IsLocal() }) {
		return ""
	}
	return "\nThis Gorunfile has no servers that run over SSH: set local = true on every task you write.\n"
}

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

// boolCopy is a "copy = true" or "copy = false" line, which small models write although copy is
// a table of files. It means nothing, so it is left out rather than failing the tasks.
var boolCopy = regexp.MustCompile(`(?m)^[ \t]*copy[ \t]*=[ \t]*(true|false)[ \t]*(#.*)?\r?\n?`)

var fence = regexp.MustCompile("(?s)```[a-zA-Z]*\\n(.*?)```")

// HasEdits reports whether text holds a task table or a schedule, the things a reply can add.
func HasEdits(text string) bool {
	return strings.Contains(text, "[tasks.") || strings.Contains(text, "[[schedule]]")
}

// stripFences takes the TOML out of a markdown code block, which small models add anyway. A reply
// may also hold shell commands in blocks of their own, so the first block with a task table or
// schedule wins.
func stripFences(reply string) string {
	blocks := fence.FindAllStringSubmatch(reply, -1)
	for _, m := range blocks {
		if HasEdits(m[1]) {
			return strings.TrimSpace(m[1])
		}
	}
	if len(blocks) > 0 {
		reply = blocks[0][1]
	}
	return strings.TrimSpace(reply)
}

// ExtractTasks returns the tasks and schedules in a model's reply as Gorunfile TOML: the first
// markdown code block holding a [tasks.<name>] table or a [[schedule]] entry, else the first
// block, or else the whole reply. It fails when they don't load, aren't valid, redefine a task or
// schedule in cfg, depend on or target a task that doesn't exist, or a task lacks a check or a
// verify. Files the tasks copy or run aren't looked for, as they may not have been written yet.
// A "copy = true" or "copy = false" line, which small models write, is left out. cfg may be nil.
func ExtractTasks(reply string, cfg *Config) (string, error) {
	return extractTasks(reply, cfg, false)
}

// ExtractTaskEdits is ExtractTasks for a reply that may also change tasks and schedules cfg has:
// a task or schedule with the name of one in cfg is its replacement.
func ExtractTaskEdits(reply string, cfg *Config) (string, error) {
	return extractTasks(reply, cfg, true)
}

func extractTasks(reply string, cfg *Config, replace bool) (string, error) {
	text := boolCopy.ReplaceAllString(stripFences(reply), "")
	if err := checkTasks(text, cfg, replace); err != nil {
		return "", err
	}
	return text + "\n", nil
}

// checkTasks reports why text isn't a set of valid tasks and schedules, new ones unless replace
// is set.
func checkTasks(text string, cfg *Config, replace bool) error {
	var file struct {
		Tasks    map[string]Task `toml:"tasks"`
		Schedule []Schedule      `toml:"schedule"`
	}
	meta, err := toml.Decode(text, &file)
	if err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return fmt.Errorf("unknown key %s", keys[0])
	}
	if len(file.Tasks) == 0 && len(file.Schedule) == 0 {
		return errors.New("no [tasks.<name>] tables or [[schedule]] entries")
	}
	var existing map[string]Task
	if cfg != nil {
		existing = cfg.Tasks
	}
	for _, name := range sortedKeys(file.Tasks) {
		task := file.Tasks[name]
		task.Name = name
		if _, ok := existing[name]; ok && !replace {
			return fmt.Errorf("task %s already exists", name)
		}
		// the files a task copies may not have been written yet
		if err := task.validateKeys(); err != nil {
			return err
		}
		for _, dep := range task.DependsOn {
			_, isNew := file.Tasks[dep]
			if _, ok := existing[dep]; !ok && !isNew {
				return fmt.Errorf("task %s depends on %s, which doesn't exist", name, dep)
			}
		}
		// a model's task must say how to tell it is done and that it worked
		if strings.TrimSpace(task.Check) == "" {
			return fmt.Errorf("task %s has no check: add check, a command that exits 0 when the step is done already", name)
		}
		if strings.TrimSpace(task.Verify) == "" {
			return fmt.Errorf("task %s has no verify: add verify, a command that exits non-zero when cmd didn't work", name)
		}
	}
	return checkSchedules(file.Schedule, file.Tasks, cfg, replace)
}

// checkSchedules reports why schedules wouldn't load in cfg with tasks added: a name used twice
// or, unless replace is set, already in cfg, or a bad timing, zone or target.
func checkSchedules(schedules []Schedule, tasks map[string]Task, cfg *Config, replace bool) error {
	c := &Config{Tasks: map[string]Task{}}
	if cfg != nil {
		maps.Copy(c.Tasks, cfg.Tasks)
		c.Bundles = cfg.Bundles
	}
	maps.Copy(c.Tasks, tasks)
	seen := map[string]bool{}
	for _, s := range schedules {
		if seen[s.Name] {
			return fmt.Errorf("schedule %q is written twice", s.Name)
		}
		seen[s.Name] = true
		if cfg != nil && !replace && slices.ContainsFunc(cfg.Schedules, func(o Schedule) bool { return o.Name == s.Name }) {
			return fmt.Errorf("schedule %q already exists", s.Name)
		}
		if err := c.validateSchedule(s); err != nil {
			return err
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

// Chat sends messages to the engine, or else the chat completions API at URL, and returns the
// model's reply.
func (a Assistant) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	if a.Engine != nil {
		return a.Engine.Chat(ctx, messages)
	}
	body, err := json.Marshal(map[string]any{"model": a.model(), "messages": messages, "temperature": 0.2})
	if err != nil {
		return "", err
	}
	url := strings.TrimSuffix(cmp.Or(a.URL, DefaultAssistantURL), "/") + "/chat/completions"
	ChatTraceFrom(ctx).Statusf("Asking %s at %s; the reply arrives when it's finished", a.model(), strings.TrimSuffix(cmp.Or(a.URL, DefaultAssistantURL), "/"))
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
