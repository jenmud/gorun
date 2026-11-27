package websrv

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/docs"
	"github.com/jenmud/gorun/internal/store"
)

// Budgets that keep a chat inside the built-in model's 8192-token context (about 4 characters a
// token), with room for its reply.
const (
	maxChatTurns    = 12    // the newest messages kept from the conversation
	maxChatChars    = 8000  // of conversation
	maxContextChars = 12000 // of Gorunfile and history
	maxOutputChars  = 1200  // of one failed task's output
	maxFileChars    = 7000  // of the Gorunfile's tasks and servers
)

// chatPrompt tells the model what it is for; the Gorunfile and history follow it.
const chatPrompt = `You are the assistant in gorun's web UI. gorun runs tasks (shell commands) on servers
over SSH, as a Gorunfile describes. Answer briefly and plainly. You can:
- answer questions about the Gorunfile's servers, tasks and bundles, listed below;
- summarise runs and diagnose why tasks failed, from the history below: name the task, the
  server and the likely cause, quote the line of output that shows it, and suggest a fix;
- write new tasks and schedules, or change the Gorunfile's tasks and schedules. Put them in one
  ` + "```toml" + ` code block of [tasks.<name>] tables and [[schedule]] entries; to change one, write
  it whole again under the same name. The page offers to save them into the Gorunfile, replacing
  the ones they change.
Only state what the context shows or what you know of gorun; say so when you don't know.

How to write tasks and schedules:
`

// private reports whether the model runs on this machine, so nothing it is sent leaves it.
func (g *generator) private() bool {
	if g == nil || g.engine != nil || g.assistant.Engine != nil {
		return true
	}
	u, err := url.Parse(g.assistant.URL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// host is where a remote model runs, for the privacy note.
func (g *generator) host() string {
	if u, err := url.Parse(g.assistant.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return g.assistant.URL
}

// assistantPage is the chat. ?run=ID starts it on that run.
func (a *app) assistantPage(w http.ResponseWriter, r *http.Request) {
	d := ui.AssistantPageData{Model: a.gen.describe(), Private: a.gen.private(), Stats: a.assistantStats()}
	if a.engine() != nil {
		m := a.modelsData("")
		d.Models = &m
	}
	if !d.Private {
		d.Host = a.gen.host()
	}
	a.mu.Lock()
	d.CanAdd = a.cfg != nil
	if a.cfg != nil {
		d.Files = configData(a.cfg, nil).Files
	}
	a.mu.Unlock()
	if id := r.URL.Query().Get("run"); id != "" && a.store != nil {
		if run, err := a.store.GetRun(r.Context(), id); err == nil && run != nil {
			d.RunID, d.RunTitle = run.ID, run.Title
		}
	}
	a.page(w, r, "Assistant", "assistant", ui.AssistantPage(d))
}

// chatRequest is what the chat page posts: the conversation so far, and the run it is about.
type chatRequest struct {
	Messages []gorun.ChatMessage `json:"messages"`
	Run      string              `json:"run"`
	// Stream asks for the reply as it is made: one JSON object a line, each a status, a piece
	// of the reply, or last, the whole chatReply. See chatEvent.
	Stream bool `json:"stream"`
}

// chatEvent is one line of a streamed chat: what the assistant is doing, a piece of the reply,
// or, with Done, the final reply.
type chatEvent struct {
	Status string     `json:"status,omitempty"`
	Token  string     `json:"token,omitempty"`
	Done   *chatReply `json:"done,omitempty"`
}

// chatReply is the model's answer, and the tasks and schedules in it when they load.
type chatReply struct {
	Reply     string `json:"reply,omitempty"`
	Tasks     string `json:"tasks,omitempty"`     // tasks and schedules, as Gorunfile TOML
	TaskNames string `json:"taskNames,omitempty"` // such as "redis-install, schedule nightly"
	Replaces  string `json:"replaces,omitempty"`  // the ones in Tasks the Gorunfile has already
	TaskError string `json:"taskError,omitempty"`
	Error     string `json:"error,omitempty"`
}

// chatHandler answers the conversation, with the Gorunfile and history as context.
func (a *app) chatHandler(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, chatReply{Error: "send the conversation as JSON"})
		return
	}
	if a.gen == nil {
		writeJSON(w, http.StatusServiceUnavailable, chatReply{Error: "the assistant is off"})
		return
	}
	send := func(chatEvent) {}
	ctx := r.Context()
	if req.Stream {
		send = streamEvents(w)
		ctx = gorun.WithChatTrace(ctx, &gorun.ChatTrace{
			Status: func(text string) { send(chatEvent{Status: text}) },
			Token:  func(piece string) { send(chatEvent{Token: piece}) },
		})
	}
	finish := func(code int, out chatReply) {
		if req.Stream {
			send(chatEvent{Done: &out})
			return
		}
		writeJSON(w, code, out)
	}

	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	send(chatEvent{Status: "Reading the Gorunfile and the recent runs"})
	conv := trimConversation(req.Messages)
	system := a.chatContext(ctx, cfg, req.Run) + mentionedTasks(cfg, conv)
	if req.Run == "" {
		system += docsFor(conv[len(conv)-1].Content)
	}
	messages := append([]gorun.ChatMessage{{Role: "system", Content: system}}, conv...)

	release, err := a.genQ.acquire(ctx, func(s string) { send(chatEvent{Status: s}) })
	if err != nil {
		finish(http.StatusServiceUnavailable, chatReply{Error: err.Error()})
		return
	}
	reply, err := a.gen.assistant.Chat(ctx, messages)
	release()
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "assistant chat", "error", err)
		}
		finish(http.StatusBadGateway, chatReply{Error: err.Error()})
		return
	}
	out := chatReply{Reply: reply}
	if hasTaskBlock(reply) {
		tasks, err := gorun.ExtractTaskEdits(reply, cfg)
		if err != nil {
			if fixed, ferr := a.repairTasks(ctx, messages, reply, err, cfg, send); ferr == nil {
				out.Reply = fixed + "\n\n(The tasks didn't load the first time, so gorun asked the model to fix them.)"
				tasks, err = gorun.ExtractTaskEdits(fixed, cfg)
			}
		}
		if err != nil {
			out.TaskError = err.Error()
		} else {
			names, scheds := editNames(tasks)
			out.Tasks = tasks
			out.TaskNames = strings.Join(append(slices.Clone(names), labelSchedules(scheds)...), ", ")
			if cfg != nil {
				names = slices.DeleteFunc(names, func(n string) bool { _, ok := cfg.Tasks[n]; return !ok })
				scheds = slices.DeleteFunc(scheds, func(n string) bool { return !inFile(cfg, kindSchedule, n) })
				out.Replaces = strings.Join(append(names, labelSchedules(scheds)...), ", ")
			}
		}
	}
	finish(http.StatusOK, out)
}

// chatRepairs is how many times the chat asks the model to fix tasks that don't load.
const chatRepairs = 2

// repairTasks asks the model to fix the tasks in reply, which don't load with err, and returns
// reply with its tasks replaced by ones that load. The model's corrections aren't streamed: the
// page shows what it is doing, then the fixed reply.
func (a *app) repairTasks(ctx context.Context, messages []gorun.ChatMessage, reply string, err error, cfg *gorun.Config, send func(chatEvent)) (string, error) {
	trace := gorun.ChatTraceFrom(ctx)
	quiet := gorun.WithChatTrace(ctx, &gorun.ChatTrace{Status: trace.Status})
	last := reply
	for try := 1; try <= chatRepairs; try++ {
		send(chatEvent{Status: fmt.Sprintf("The tasks don't load (%s). Asking the model to fix them, try %d of %d", shortError(err), try, chatRepairs)})
		ask := append(slices.Clone(messages),
			gorun.ChatMessage{Role: "assistant", Content: last},
			gorun.ChatMessage{Role: "user", Content: "Those don't load: " + err.Error() + ". Write all the tasks and schedules again, corrected, in one ```toml block and nothing else. Use only the keys listed above. Put any program, page or config in a [[tasks.<name>.copy]] table's content and run the file from cmd, rather than writing it into cmd with python3 -c or a heredoc."},
		)
		release, cerr := a.genQ.acquire(ctx, func(s string) { send(chatEvent{Status: s}) })
		if cerr != nil {
			return "", cerr
		}
		fix, cerr := a.gen.assistant.Chat(quiet, ask)
		release()
		if cerr != nil {
			return "", cerr
		}
		if _, err = gorun.ExtractTaskEdits(fix, cfg); err == nil {
			return replaceTaskBlock(reply, fix), nil
		}
		last = fix
	}
	return "", err
}

// replaceTaskBlock puts the task block of fix in place of the one in reply, keeping the rest of
// reply, such as its explanation.
func replaceTaskBlock(reply, fix string) string {
	block := ""
	for _, m := range codeBlock.FindAllStringSubmatch(fix, -1) {
		if gorun.HasEdits(m[1]) {
			block = m[0]
			break
		}
	}
	if block == "" {
		block = "```toml\n" + strings.TrimSpace(fix) + "\n```"
	}
	for _, loc := range codeBlock.FindAllStringSubmatchIndex(reply, -1) {
		if gorun.HasEdits(reply[loc[2]:loc[3]]) {
			return reply[:loc[0]] + block + reply[loc[1]:]
		}
	}
	return block
}

// shortError is the first line of err, cut short, for a status line.
func shortError(err error) string {
	msg, _, _ := strings.Cut(err.Error(), "\n")
	if len(msg) > 90 {
		msg = msg[:90] + "…"
	}
	return msg
}

// codeBlock is a fenced code block in a reply, and its text.
var codeBlock = regexp.MustCompile("(?s)```[a-zA-Z]*\n(.*?)```")

// hasTaskBlock reports whether a reply has a code block with a task table or a schedule. A reply
// that only names one in its prose, such as "[tasks.site] deploys the page", holds none to offer.
func hasTaskBlock(reply string) bool {
	for _, m := range codeBlock.FindAllStringSubmatch(reply, -1) {
		if gorun.HasEdits(m[1]) {
			return true
		}
	}
	return false
}

// streamEvents starts a streamed chat reply on w and returns what sends each event, flushed at
// once so the page sees it.
func streamEvents(w http.ResponseWriter) func(chatEvent) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // keep proxies such as nginx from holding it back
	w.WriteHeader(http.StatusOK)
	// a ResponseController finds the Flusher through writers that wrap w, such as OpenTelemetry's
	rc := http.NewResponseController(w)
	enc := json.NewEncoder(w)
	return func(ev chatEvent) {
		if enc.Encode(ev) == nil {
			_ = rc.Flush()
		}
	}
}

// addTasksHandler saves the tasks and schedules a chat reply wrote straight into the Gorunfile,
// as the Tasks page's edits are: there is nothing left to review on the Config page. One with
// unsaved changes there is left alone.
func (a *app) addTasksHandler(w http.ResponseWriter, r *http.Request) {
	var req struct{ Tasks, File string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, chatReply{Error: "send the tasks as JSON"})
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	cfg, pending := a.cfg, slices.Clone(a.pending)
	a.mu.Unlock()
	if cfg == nil {
		writeJSON(w, http.StatusConflict, chatReply{Error: "load a Gorunfile first"})
		return
	}
	names, changes, err := taskChanges(cfg, nil, req.Tasks, req.File, true)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, chatReply{Error: err.Error()})
		return
	}
	for _, ch := range changes {
		if msg := pendingConflict(pending, ch.Kind, ch.Name); msg != "" {
			writeJSON(w, http.StatusConflict, chatReply{Error: msg})
			return
		}
	}
	if err := a.saveDirect(req.File, changes); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, chatReply{Error: "could not save them: " + err.Error()})
		return
	}
	// a local task, or a schedule of one, needs a local server to run on, as the forms' saves do
	note, err := a.addLocalServerIfNeeded(changes)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, chatReply{Error: "saved them, but " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, chatReply{Reply: fmt.Sprintf("Saved %s into %s.", strings.Join(names, ", "), rel(cfg.Path, req.File)) + note})
}

// queueTasks checks the tasks and schedules in text against the loaded file and queues them as
// unsaved changes; see taskChanges. a.mu must be held.
func (a *app) queueTasks(text, file string, replace bool) ([]string, error) {
	names, changes, err := taskChanges(a.cfg, a.pending, text, file, replace)
	if err != nil {
		return nil, err
	}
	a.pending = append(a.pending, changes...)
	return names, nil
}

// taskChanges checks the tasks and schedules in text against cfg and the unsaved changes in
// pending, and returns their names (schedules as "schedule <name>") and the changes that save them
// into file. With replace, one that is there already is changed where it is defined; without, it
// is an error.
func taskChanges(cfg *gorun.Config, pending []gorun.Change, text, file string, replace bool) ([]string, []gorun.Change, error) {
	if !slices.Contains(cfg.Files, file) {
		return nil, nil, fmt.Errorf("pick the file to save into")
	}
	if _, err := gorun.ExtractTaskEdits(text, cfg); err != nil {
		return nil, nil, err
	}
	var doc struct {
		Tasks    map[string]map[string]any `toml:"tasks"`
		Schedule []map[string]any          `toml:"schedule"`
	}
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, nil, err
	}
	type entity struct {
		kind, name string
		body       map[string]any
	}
	var all []entity
	for _, name := range slices.Sorted(maps.Keys(doc.Tasks)) {
		all = append(all, entity{kindTask, name, doc.Tasks[name]})
	}
	for _, body := range doc.Schedule {
		name, _ := body["name"].(string)
		all = append(all, entity{kindSchedule, name, body})
	}
	for _, e := range all {
		if err := validName(e.kind, e.name); err != nil {
			return nil, nil, err
		}
		if !replace && present(cfg, pending, e.kind, e.name) {
			return nil, nil, fmt.Errorf("a %s named %s already exists", kindNames[e.kind], e.name)
		}
	}
	var names []string
	var changes []gorun.Change
	for _, e := range all {
		names = append(names, map[string]string{kindTask: e.name, kindSchedule: "schedule " + e.name}[e.kind])
		if present(cfg, pending, e.kind, e.name) {
			src := sourceOf(cfg, pending, e.kind, e.name)
			changes = append(changes,
				gorun.Change{Kind: e.kind, Name: e.name, File: src, Remove: true},
				gorun.Change{Kind: e.kind, Name: e.name, File: src, Body: e.body},
			)
			continue
		}
		changes = append(changes, gorun.Change{Kind: e.kind, Name: e.name, File: file, Body: e.body})
	}
	return names, changes, nil
}

// Budgets for the docs a question is sent with: they share the context with everything else.
const (
	maxDocSections = 2
	maxDocChars    = 2000
)

// requestWords start a request, such as for tasks or a schedule, which docs don't help answer.
var requestWords = map[string]bool{"write": true, "add": true, "create": true, "install": true, "set": true, "setup": true,
	"run": true, "schedule": true, "change": true, "move": true, "make": true, "edit": true, "remove": true, "delete": true,
	"give": true, "put": true, "deploy": true, "configure": true, "update": true, "rename": true, "point": true, "generate": true}

// docsFor is the parts of gorun's docs that answer question, so the model answers from them
// rather than from memory. Requests, such as for tasks, and questions about runs get none.
func docsFor(question string) string {
	q := strings.TrimSpace(question)
	first, _, _ := strings.Cut(strings.ToLower(q), " ")
	if requestWords[strings.Trim(first, ",.:")] && !strings.HasSuffix(q, "?") {
		return ""
	}
	// questions about runs are answered from the history, which takes the room
	low := strings.ToLower(q)
	for _, w := range []string{"this run", "last run", "recent run", "latest run", "the runs", "summar", "why did"} {
		if strings.Contains(low, w) {
			return ""
		}
	}
	found := docs.Search(q, maxDocSections, maxDocChars)
	if len(found) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## From gorun's docs\nAnswer questions about gorun from these. Don't name options, flags or settings they and the rest of this context don't show; when they don't cover the question, say so.\n")
	for _, sec := range found {
		text := sec.Text
		if len(text) > maxDocChars {
			text = text[:maxDocChars] + "…"
		}
		fmt.Fprintf(&b, "\n### %s\n%s\n", sec.Title, text)
	}
	return b.String()
}

// maxMentioned caps the tasks whose TOML goes in the context, so it stays small.
const maxMentioned = 3

// mentionedTasks writes out, as TOML, the tasks and schedules the newest question names, so the
// model can change them without guessing what they hold now.
func mentionedTasks(cfg *gorun.Config, conv []gorun.ChatMessage) string {
	if cfg == nil || len(conv) == 0 {
		return ""
	}
	question := strings.ToLower(conv[len(conv)-1].Content)
	var b strings.Builder
	n := 0
	write := func(c *gorun.Config) {
		data, err := c.Marshal()
		if err != nil {
			return
		}
		if n == 0 {
			b.WriteString("\n## Tasks and schedules the question names, as they are now\nTo change one, write it whole again under the same name.\n```toml\n")
		}
		b.Write(data)
		n++
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Tasks)) {
		if n < maxMentioned && mentions(question, strings.ToLower(name)) {
			write(&gorun.Config{Tasks: map[string]gorun.Task{name: cfg.Tasks[name]}})
		}
	}
	for _, s := range cfg.Schedules {
		if n < maxMentioned && mentions(question, strings.ToLower(s.Name)) {
			write(&gorun.Config{Schedules: []gorun.Schedule{s}})
		}
	}
	if n > 0 {
		b.WriteString("```\n")
	}
	return b.String()
}

// mentions reports whether name appears in text as a word of its own.
func mentions(text, name string) bool {
	for i := strings.Index(text, name); i >= 0; {
		end := i + len(name)
		if (i == 0 || !wordByte(text[i-1])) && (end == len(text) || !wordByte(text[end])) {
			return true
		}
		next := strings.Index(text[i+1:], name)
		if next < 0 {
			return false
		}
		i += 1 + next
	}
	return false
}

func wordByte(c byte) bool {
	return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z'
}

// editNames lists the tasks and the schedules in Gorunfile TOML.
func editNames(text string) (tasks, schedules []string) {
	var doc struct {
		Tasks    map[string]any `toml:"tasks"`
		Schedule []struct{ Name string }
	}
	_, _ = toml.Decode(text, &doc)
	for _, s := range doc.Schedule {
		schedules = append(schedules, s.Name)
	}
	return slices.Sorted(maps.Keys(doc.Tasks)), schedules
}

// labelSchedules names schedules for a message, such as "schedule nightly".
func labelSchedules(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "schedule " + n
	}
	return out
}

// trimConversation keeps the newest user and assistant messages that fit the budget, starting
// on a user message.
func trimConversation(msgs []gorun.ChatMessage) []gorun.ChatMessage {
	var kept []gorun.ChatMessage
	size := 0
	for i := len(msgs) - 1; i >= 0 && len(kept) < maxChatTurns; i-- {
		m := msgs[i]
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if size += len(m.Content); size > maxChatChars && len(kept) > 0 {
			break
		}
		kept = append(kept, m)
	}
	slices.Reverse(kept)
	for len(kept) > 1 && kept[0].Role != "user" {
		kept = kept[1:]
	}
	return kept
}

// chatContext is the system prompt: what the assistant does, then the Gorunfile and history.
// Env vars and template variables are left out of the history, as they may hold secrets.
func (a *app) chatContext(ctx context.Context, cfg *gorun.Config, runID string) string {
	var b strings.Builder
	b.WriteString(chatPrompt)
	b.WriteString(gorun.TaskFormat)
	b.WriteString("\n\n")
	budget := func() int { return len(chatPrompt) + len(gorun.TaskFormat) + maxContextChars - b.Len() }

	if cfg == nil {
		b.WriteString("No Gorunfile is loaded, so there are no tasks to depend on and new tasks can't be added.\n")
	} else {
		writeGorunfile(&b, cfg)
	}
	if a.store == nil {
		b.WriteString("\nThe run history isn't available.\n")
		return b.String()
	}
	runs, err := a.runList(ctx)
	if err != nil || len(runs) == 0 {
		b.WriteString("\nNo runs have been saved yet.\n")
		return b.String()
	}
	b.WriteString("\n## Recent runs, newest first\n")
	for _, r := range runs[:min(len(runs), 10)] {
		writeRunLine(&b, r)
	}
	// the run asked about, or else the newest, in full
	focus := runs[0].ID
	if runID != "" {
		focus = runID
	}
	if run, err := a.store.GetRun(ctx, focus); err == nil && run != nil {
		a.writeRunDetail(ctx, &b, cfg, run, budget)
	}
	return b.String()
}

func writeGorunfile(b *strings.Builder, cfg *gorun.Config) {
	start := b.Len()
	fmt.Fprintf(b, "## The Gorunfile %s\n", cmp.Or(cfg.Title, cfg.Path))
	names := slices.Sorted(maps.Keys(cfg.Tasks))
	fmt.Fprintf(b, "Tasks (%d):\n", len(names))
	for _, name := range names[:min(len(names), 40)] {
		t := cfg.Tasks[name]
		fmt.Fprintf(b, "- %s: %s", name, t.Description)
		if t.Local {
			b.WriteString(" (local only)")
		}
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(b, " (depends on %s)", strings.Join(t.DependsOn, ", "))
		}
		b.WriteString("\n")
	}
	if len(names) > 40 {
		fmt.Fprintf(b, "- and %d more\n", len(names)-40)
	}
	if len(cfg.Schedules) > 0 {
		fmt.Fprintf(b, "Schedules (%d):\n", len(cfg.Schedules))
		for _, s := range cfg.Schedules[:min(len(cfg.Schedules), 20)] {
			when := "cron " + strconv.Quote(s.Cron)
			if s.At != "" {
				when = "once at " + s.At
			}
			fmt.Fprintf(b, "- %s: runs %s, %s, %s", s.Name, s.Target, when, cmp.Or(s.Timezone, "UTC"))
			if s.Missed != "" {
				fmt.Fprintf(b, ", missed = %s", s.Missed)
			}
			b.WriteString("\n")
		}
	}
	bundles := slices.Sorted(maps.Keys(cfg.Bundles))
	if len(bundles) > 0 {
		b.WriteString("Bundles (use them as @name):\n")
		for _, name := range bundles[:min(len(bundles), 30)] {
			fmt.Fprintf(b, "- %s: %s\n", name, strings.Join(cfg.Bundles[name].Tasks, ", "))
		}
	}
	fmt.Fprintf(b, "Servers (%d):\n", len(cfg.Servers))
	for i, s := range cfg.Servers[:min(len(cfg.Servers), 30)] {
		if b.Len()-start > maxFileChars {
			fmt.Fprintf(b, "- and %d more servers; gorun Gorunfile -list-servers lists them all\n", len(cfg.Servers)-i)
			return
		}
		kind := "over SSH"
		if s.IsLocal() {
			kind = "on this machine"
		}
		fmt.Fprintf(b, "- %s at %s, runs %s", s.Name, s.Address(), kind)
		if len(s.Groups) > 0 {
			fmt.Fprintf(b, ", groups %s", strings.Join(s.Groups, ", "))
		}
		if len(s.Tags) > 0 {
			fmt.Fprintf(b, ", tags %s", strings.Join(s.Tags, ", "))
		}
		if p, err := cfg.ServerPipeline(s); err == nil {
			names := make([]string, 0, len(p.Tasks))
			for _, t := range p.Tasks[:min(len(p.Tasks), 15)] {
				names = append(names, t.Name)
			}
			if len(p.Tasks) > 15 {
				names = append(names, fmt.Sprintf("and %d more", len(p.Tasks)-15))
			}
			fmt.Fprintf(b, "; its tasks, in run order: %s", cmp.Or(strings.Join(names, ", "), "none"))
		}
		b.WriteString("\n")
	}
	b.WriteString(gorun.LocalNote(cfg))
}

func writeRunLine(b *strings.Builder, r store.RunSummary) {
	status := "passed"
	switch {
	case !r.Finished:
		status = "running"
	case !r.Success:
		status = "failed"
	}
	fmt.Fprintf(b, "- run %s %q started %s: %s, %d tasks, %d failed, %d skipped, took %s",
		r.ID, r.Title, r.StartedAt.Format(time.DateTime), status, r.TotalTasks, r.Failed, r.Skipped, r.Duration.Round(time.Second))
	if r.Schedule != "" {
		fmt.Fprintf(b, ", by schedule %s", r.Schedule)
	}
	if r.CheckOnly {
		b.WriteString(", check only")
	}
	b.WriteString("\n")
}

// stateNames spell out each task state, so a small model doesn't guess why a task was skipped.
var stateNames = map[int]string{
	store.StatePending:   "pending (never started)",
	store.StateRunning:   "still running",
	store.StateDone:      "done",
	store.StateFailed:    "failed",
	store.StateSkipped:   "skipped because a task it depends on failed",
	store.StateUnchanged: "unchanged: its check said there was nothing to do",
	store.StateWouldRun:  "would run (a check-only run)",
}

// writeRunDetail writes a run's servers and tasks, why skipped ones were skipped, with the command and output of each failed
// task, while budget allows.
func (a *app) writeRunDetail(ctx context.Context, b *strings.Builder, cfg *gorun.Config, run *store.RunDetail, budget func() int) {
	fmt.Fprintf(b, "\n## Run %s in detail\n", run.ID)
	for _, srv := range slices.Sorted(maps.Keys(run.ServerErrors)) {
		fmt.Fprintf(b, "Server %s could not run its tasks: %s\n", srv, run.ServerErrors[srv])
	}
	var failed []store.TaskDetail
	state := map[[2]string]int{} // by server and task
	for _, t := range run.Tasks {
		state[[2]string{t.Server, t.Name}] = t.State
		if t.State == store.StateFailed {
			failed = append(failed, t)
		}
	}
	// a big run is counted per server, and only its tasks that didn't pass are listed
	listed := run.Tasks
	if len(run.Tasks) > maxListedTasks {
		writeServerCounts(b, run.Tasks)
		listed = slices.DeleteFunc(slices.Clone(run.Tasks), func(t store.TaskDetail) bool {
			return t.State == store.StateDone || t.State == store.StateUnchanged
		})
		if len(listed) > maxListedTasks {
			fmt.Fprintf(b, "(%d more tasks that didn't pass are left out for space)\n", len(listed)-maxListedTasks)
			listed = listed[:maxListedTasks]
		}
	}
	for _, t := range listed {
		fmt.Fprintf(b, "- %s on %s: %s", t.Name, t.Server, stateNames[t.State])
		// name the dependency that failed: a small model can't follow the chain itself
		if t.State == store.StateSkipped && cfg != nil {
			var why []string
			for _, d := range cfg.Tasks[t.Name].DependsOn {
				switch state[[2]string{t.Server, d}] {
				case store.StateFailed:
					why = append(why, d+" failed")
				case store.StateSkipped:
					why = append(why, d+" was skipped")
				}
			}
			if len(why) > 0 {
				fmt.Fprintf(b, " (%s)", strings.Join(why, ", "))
			}
		}
		fmt.Fprintf(b, ", took %s\n", t.Duration.Round(time.Millisecond))
	}
	for _, t := range failed {
		if budget() < 600 {
			b.WriteString("(more failed tasks left out for space)\n")
			return
		}
		full, err := a.store.GetTask(ctx, run.ID, t.Server, t.Name)
		if err != nil {
			continue
		}
		fmt.Fprintf(b, "\n### %s on %s failed with exit code %d\ncommand: %s\n", t.Name, t.Server, full.ExitCode, tail(full.Command, 400))
		if full.Stderr != "" {
			fmt.Fprintf(b, "stderr (end):\n%s\n", tail(full.Stderr, maxOutputChars))
		}
		if full.Stdout != "" {
			fmt.Fprintf(b, "stdout (end):\n%s\n", tail(full.Stdout, maxOutputChars/2))
		}
		for _, step := range []struct {
			name string
			s    *store.Step
		}{{"check", full.Check}, {"verify", full.Verify}} {
			if step.s != nil {
				fmt.Fprintf(b, "%s: exit %d %s\n", step.name, step.s.ExitCode, tail(step.s.Stderr+step.s.Stdout, 300))
			}
		}
	}
}

// maxListedTasks is how many of a run's tasks the context lists one by one.
const maxListedTasks = 40

var shortStates = map[int]string{
	store.StatePending: "pending", store.StateRunning: "running", store.StateDone: "done", store.StateFailed: "failed",
	store.StateSkipped: "skipped", store.StateUnchanged: "unchanged", store.StateWouldRun: "would run",
}

// writeServerCounts writes how many of each server's tasks ended in each state.
func writeServerCounts(b *strings.Builder, tasks []store.TaskDetail) {
	var servers []string
	counts := map[string]map[int]int{}
	for _, t := range tasks {
		if counts[t.Server] == nil {
			servers = append(servers, t.Server)
			counts[t.Server] = map[int]int{}
		}
		counts[t.Server][t.State]++
	}
	for _, s := range servers {
		var parts []string
		total := 0
		for _, st := range slices.Sorted(maps.Keys(counts[s])) {
			total += counts[s][st]
			parts = append(parts, fmt.Sprintf("%d %s", counts[s][st], shortStates[st]))
		}
		fmt.Fprintf(b, "- %s ran %d tasks: %s\n", s, total, strings.Join(parts, ", "))
	}
	b.WriteString("The tasks that didn't pass:\n")
}

// tail is the end of s, at most n bytes, cut at a line start when it can be.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < n/2 {
		s = s[i+1:]
	}
	return "…" + s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
