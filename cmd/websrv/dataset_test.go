//go:build dataset

package websrv

// The assistant's fine-tuning data, built from training/seeds by sending every example through
// the real prompts (GenerateTasks and the chat handler), and an evaluation of a model on the
// held-out part. See training/README.md.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/llm"
	"github.com/jenmud/gorun/internal/store"
)

const (
	repoRoot = "../.."
	seedDir  = repoRoot + "/training/seeds"
	dataDir  = repoRoot + "/training/data"
	outDir   = repoRoot + "/training/out"
)

// dsScenarios are the Gorunfiles examples are set in, besides training/seeds/scenarios.
var dsScenarios = []string{
	"recipes/Gorunfile",
	"recipes/01-parallel-basics/Gorunfile",
	"recipes/02-dependency-dag/Gorunfile",
	"recipes/03-mega-pipeline/Gorunfile",
	"recipes/04-dev-server/Gorunfile",
	"recipes/05-scheduled-jobs/Gorunfile",
	"recipes/07-big-upload/Gorunfile",
	"recipes/large-inventory/Gorunfile",
	"recipes/local-run/Gorunfile",
	"docs/tutorial/Gorunfile",
	"examples/rollout/Gorunfile",
	"examples/servicedeck/Gorunfile",
}

type taskSeed struct {
	ID      string              `toml:"id"`
	Request []string            `toml:"request"`
	Note    string              `toml:"note"`
	Answer  string              `toml:"answer"`
	Params  []map[string]string `toml:"params"`
}

type qaSeed struct {
	Q    []string `toml:"q"`
	A    string   `toml:"a"`
	Want []string `toml:"want"`
}

// output is what a read-only command prints, for simulated runs, and what it shows.
type output struct {
	Match   []string `toml:"match"`
	Stdout  string   `toml:"stdout"`
	Summary string   `toml:"summary"`
	Want    []string `toml:"want"`
}

type failure struct {
	ID        string   `toml:"id"`
	Match     []string `toml:"match"`
	NeedsSudo bool     `toml:"needs_sudo"`
	NoSudo    bool     `toml:"no_sudo"`
	HasVerify bool     `toml:"has_verify"`
	Exit      int      `toml:"exit"`
	Stderr    string   `toml:"stderr"`
	Quote     string   `toml:"quote"`
	Cause     string   `toml:"cause"`
	Fix       string   `toml:"fix"`
}

// dsExample is one line of the data: a conversation whose last message is the reply to learn.
type dsExample struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Scenario string              `json:"scenario,omitempty"`
	Messages []gorun.ChatMessage `json:"messages"`
	Want     []string            `json:"want,omitempty"`
	Tasks    bool                `json:"tasks,omitempty"` // the reply must hold tasks that load
}

type dsScenario struct {
	name string // the Gorunfile's path from the repository root, "" for none
	app  *app
	rec  *seqEngine
	cfg  *gorun.Config
}

// seqEngine replies with replies in turn, and keeps the last conversation it was sent.
type seqEngine struct {
	replies []string
	got     []gorun.ChatMessage
}

func (e *seqEngine) Chat(_ context.Context, m []gorun.ChatMessage) (string, error) {
	e.got = slices.Clone(m)
	r := "ok"
	if len(e.replies) > 0 {
		r, e.replies = e.replies[0], e.replies[1:]
	}
	return r, nil
}

type builder struct {
	t           *testing.T
	ctx         context.Context
	rng         *rand.Rand
	scenarios   []*dsScenario
	qa          []qaSeed
	outputs     []output
	train, eval []dsExample
	counts      map[string]int
}

// TestBuildDataset writes training/data/train.jsonl and eval.jsonl.
func TestBuildDataset(t *testing.T) {
	b := &builder{t: t, ctx: context.Background(), rng: rand.New(rand.NewPCG(7, 11)), counts: map[string]int{}}
	b.loadScenarios()

	var gen struct {
		Task []taskSeed `toml:"task"`
	}
	var qa struct {
		QA []qaSeed `toml:"qa"`
	}
	var fails struct {
		Failure []failure `toml:"failure"`
	}
	var outs struct {
		Output []output `toml:"output"`
	}
	for file, v := range map[string]any{"generate.toml": &gen, "qa.toml": &qa, "failures.toml": &fails, "outputs.toml": &outs} {
		if _, err := toml.DecodeFile(filepath.Join(seedDir, file), v); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	b.qa = qa.QA
	b.outputs = outs.Output

	for _, s := range gen.Task {
		b.taskExamples(s)
	}
	for _, q := range qa.QA {
		b.qaExamples(q)
	}
	for _, sc := range b.scenarios {
		if sc.cfg != nil {
			b.gorunfileExamples(sc)
			b.scheduleExamples(sc)
			b.taskEditExamples(sc)
			for range b.runChatRounds(sc) {
				b.runChatExamples(sc)
			}
		}
	}
	for _, sc := range b.scenarios {
		if sc.cfg == nil || !hasSSH(sc.cfg) {
			continue
		}
		for range 3 {
			for _, f := range fails.Failure {
				b.runExamples(sc, f)
			}
		}
		for range 4 {
			b.serverErrorExamples(sc)
			b.passedRunExamples(sc)
		}
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	b.rng.Shuffle(len(b.train), func(i, j int) { b.train[i], b.train[j] = b.train[j], b.train[i] })
	writeJSONL(t, filepath.Join(dataDir, "train.jsonl"), b.train)
	writeJSONL(t, filepath.Join(dataDir, "eval.jsonl"), b.eval)
	t.Logf("train %d, eval %d, by kind %v", len(b.train), len(b.eval), b.counts)
}

func (b *builder) loadScenarios() {
	paths := slices.Clone(dsScenarios)
	own, _ := filepath.Glob(filepath.Join(seedDir, "scenarios", "*", "Gorunfile"))
	for _, p := range own {
		rel, _ := filepath.Rel(repoRoot, p)
		paths = append(paths, rel)
	}
	b.scenarios = append(b.scenarios, b.newScenario(""))
	for _, p := range paths {
		b.scenarios = append(b.scenarios, b.newScenario(p))
	}
}

func (b *builder) newScenario(path string) *dsScenario {
	t := b.t
	st, err := store.Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := &app{store: st, hub: &hub{clients: map[chan string]struct{}{}}, runs: map[string]context.CancelFunc{}}
	if path != "" {
		if err := a.loadConfig(filepath.Join(repoRoot, path)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Cleanup(a.stopScheduler)
	}
	rec := &seqEngine{}
	a.gen = &generator{assistant: gorun.Assistant{Engine: rec}}
	return &dsScenario{name: path, app: a, rec: rec, cfg: a.cfg}
}

func (b *builder) pick() *dsScenario { return b.scenarios[b.rng.IntN(len(b.scenarios))] }

// add files ex under train or eval; about one in eight keys is held out.
func (b *builder) add(ex dsExample, key string) {
	h := fnv.New32a()
	h.Write([]byte(key))
	b.counts[ex.Kind]++
	if h.Sum32()%8 == 0 {
		b.eval = append(b.eval, ex)
		return
	}
	b.train = append(b.train, ex)
}

// chat is the conversation the chat page would send the model, with reply as its answer.
func (b *builder) chat(sc *dsScenario, conv []gorun.ChatMessage, run, reply string) []gorun.ChatMessage {
	sc.rec.replies = []string{reply}
	out := postJSON(b.t, sc.app, "/assistant/chat", chatRequest{Messages: conv, Run: run})
	if out.Error != "" {
		b.t.Fatalf("chat in %s: %s", sc.name, out.Error)
	}
	return append(sc.rec.got, gorun.ChatMessage{Role: "assistant", Content: reply})
}

// withPrefix sometimes starts the conversation with an earlier question and answer, so the
// model learns to answer the newest message.
func (b *builder) withPrefix(q string) []gorun.ChatMessage {
	conv := []gorun.ChatMessage{}
	if b.rng.IntN(5) == 0 {
		p := b.qa[b.rng.IntN(len(b.qa))]
		conv = append(conv, gorun.ChatMessage{Role: "user", Content: p.Q[0]}, gorun.ChatMessage{Role: "assistant", Content: p.A})
	}
	return append(conv, gorun.ChatMessage{Role: "user", Content: q})
}

func fill(s string, p map[string]string) string {
	for k, v := range p {
		s = strings.ReplaceAll(s, "<<"+k+">>", v)
	}
	return s
}

var descLine = regexp.MustCompile(`(?m)^description = .*$`)

// dependOn makes the first task in answer depend on dep, when it has no depends_on yet.
func dependOn(answer, dep string) (string, bool) {
	first := strings.Index(answer, "[tasks.")
	next := strings.Index(answer[first+1:], "\n[tasks.")
	end := len(answer)
	if next >= 0 {
		end = first + 1 + next
	}
	if strings.Contains(answer[first:end], "depends_on") {
		return "", false
	}
	loc := descLine.FindStringIndex(answer[first:end])
	if loc == nil {
		return "", false
	}
	at := first + loc[1]
	return answer[:at] + "\ndepends_on = [\"" + dep + "\"]" + answer[at:], true
}

// listedTasks are the tasks the chat context names, which the model can depend on.
func listedTasks(cfg *gorun.Config, local bool) []string {
	names := slices.Sorted(maps.Keys(cfg.Tasks))
	names = names[:min(len(names), 30)]
	return slices.DeleteFunc(names, func(n string) bool { return cfg.Tasks[n].Local != local })
}

func (b *builder) taskExamples(s taskSeed) {
	params := s.Params
	if len(params) == 0 {
		params = []map[string]string{{}}
	}
	for pi, p := range params {
		answer := strings.TrimSpace(fill(s.Answer, p))
		note := fill(s.Note, p)
		if _, err := gorun.ExtractTasks(answer, nil); err != nil {
			b.t.Fatalf("seed %s: %v\n%s", s.ID, err, answer)
		}
		local := strings.Contains(answer, "local = true")
		for qi, q := range s.Request {
			q = fill(q, p)
			id := fmt.Sprintf("%s/%d/%d", s.ID, pi, qi)
			for range 2 {
				b.genExample(id, q, answer)
				b.chatGenExample(id, q, answer, note)
			}
			// the same request, run at set times
			if b.rng.IntN(3) == 0 {
				sAnswer, say, sched := b.withSchedule(answer)
				sNote := note + " " + scheduleNote(sched, sched.Target)
				sq := q + ", and run it" + say
				b.chatGenExample(id+"/sched", sq, sAnswer, sNote)
				b.genExample(id+"/sched", sq, sAnswer)
			}
			// the same request, after a task the Gorunfile already has
			sc := b.pick()
			if sc.cfg == nil {
				continue
			}
			deps := listedTasks(sc.cfg, local || !hasSSH(sc.cfg))
			if len(deps) == 0 || b.rng.IntN(3) != 0 {
				continue
			}
			dep := deps[b.rng.IntN(len(deps))]
			dAnswer, ok := dependOn(localIn(sc, answer), dep)
			if !ok {
				continue
			}
			if _, err := gorun.ExtractTasks(dAnswer, sc.cfg); err != nil {
				continue
			}
			dq := q + ", after " + dep
			dNote := note + " It depends on " + dep + ", which the Gorunfile already has, so it runs after it."
			if b.rng.IntN(2) == 0 {
				b.genIn(sc, id+"/dep", dq, dAnswer, nil)
			} else {
				b.add(dsExample{ID: id + "/dep", Kind: "chat-gen", Scenario: sc.name, Tasks: true,
					Messages: b.chat(sc, b.withPrefix(dq), "", chatTasks(dNote, dAnswer))}, id+"/dep")
			}
		}
	}
}

var taskHeader = regexp.MustCompile(`(?m)^\[tasks\.[^\]]+\]$`)

// localIn marks every task in answer local = true when sc has no SSH server, as LocalNote tells
// the model to.
func localIn(sc *dsScenario, answer string) string {
	if sc.cfg == nil || hasSSH(sc.cfg) {
		return answer
	}
	blocks := taskHeader.FindAllStringIndex(answer, -1)
	for i := len(blocks) - 1; i >= 0; i-- {
		end := len(answer)
		if i+1 < len(blocks) {
			end = blocks[i+1][0]
		}
		if strings.Contains(answer[blocks[i][0]:end], "\nlocal = true") {
			continue
		}
		at := blocks[i][1]
		answer = answer[:at] + "\nlocal = true" + answer[at:]
	}
	return answer
}

func chatTasks(note, answer string) string {
	return note + "\n\n```toml\n" + answer + "\n```"
}

// genExample is the request through GenerateTasks, in a scenario where the tasks are new.
func (b *builder) genExample(id, q, answer string) {
	for range 10 {
		sc := b.pick()
		answer := localIn(sc, answer)
		if _, err := gorun.ExtractTasks(answer, sc.cfg); err != nil {
			continue
		}
		var broken []string
		if b.rng.IntN(6) == 0 {
			if bad, ok := breakTasks(answer, b.rng); ok {
				if _, err := gorun.ExtractTasks(bad, sc.cfg); err != nil {
					broken = []string{bad}
				}
			}
		}
		b.genIn(sc, id, q, answer, broken)
		return
	}
}

func (b *builder) genIn(sc *dsScenario, id, q, answer string, broken []string) {
	sc.rec.replies = append(slices.Clone(broken), answer)
	if _, err := (gorun.Assistant{Engine: sc.rec}).GenerateTasks(b.ctx, q, sc.cfg); err != nil {
		b.t.Fatalf("%s in %s: %v", id, sc.name, err)
	}
	kind := "gen"
	if len(broken) > 0 {
		kind = "gen-fix"
	}
	b.add(dsExample{ID: id, Kind: kind, Scenario: sc.name, Tasks: true,
		Messages: append(sc.rec.got, gorun.ChatMessage{Role: "assistant", Content: answer})}, id)
}

// breakTasks makes a mistake a small model makes, for the examples that correct one.
func breakTasks(answer string, rng *rand.Rand) (string, bool) {
	switch rng.IntN(3) {
	case 0:
		if strings.Contains(answer, "\ncmd = ") {
			return strings.Replace(answer, "\ncmd = ", "\ncommand = ", 1), true
		}
	case 1:
		return dependOn(answer, "setup")
	}
	return "Here is the task:\n" + strings.ReplaceAll(answer, "[tasks.", "[task."), true
}

func (b *builder) chatGenExample(id, q, answer, note string) {
	for range 10 {
		sc := b.pick()
		answer := localIn(sc, answer)
		if _, err := gorun.ExtractTasks(answer, sc.cfg); err != nil {
			continue
		}
		b.add(dsExample{ID: id + "/chat", Kind: "chat-gen", Scenario: sc.name, Tasks: true,
			Messages: b.chat(sc, b.withPrefix(q), "", chatTasks(note, answer))}, id+"/chat")
		return
	}
}

func (b *builder) qaExamples(q qaSeed) {
	for i, text := range q.Q {
		for range 4 {
			sc := b.pick()
			b.clearRuns(sc)
			if sc.cfg != nil && hasSSH(sc.cfg) && b.rng.IntN(2) == 0 {
				b.saveRun(sc, b.simulate(sc, nil, nil, ""), time.Now())
			}
			id := fmt.Sprintf("qa/%s/%d", slug(q.Q[0]), i)
			b.add(dsExample{ID: id, Kind: "qa", Scenario: sc.name, Want: q.Want,
				Messages: b.chat(sc, b.withPrefix(text), "", q.A)}, id)
		}
	}
}

func slug(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")[:min(len(strings.Trim(s, "-")), 40)]
}

func hasSSH(cfg *gorun.Config) bool {
	return slices.ContainsFunc(cfg.Servers, func(s gorun.Server) bool { return !s.IsLocal() })
}

// pipelines are the servers the chat context lists, with their tasks in run order.
func pipelines(cfg *gorun.Config) []gorun.Server {
	var ctx strings.Builder
	writeGorunfile(&ctx, cfg)
	var out []gorun.Server
	for _, s := range cfg.Servers {
		if !strings.Contains(ctx.String(), "\n- "+s.Name+" at ") {
			continue
		}
		if p, err := cfg.ServerPipeline(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func taskNamesOf(s gorun.Server) []string {
	names := make([]string, len(s.Tasks))
	for i, t := range s.Tasks {
		names[i] = t.Name
	}
	return names
}

func list(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func one[T any](rng *rand.Rand, xs []T) T { return xs[rng.IntN(len(xs))] }

// gorunfileExamples ask about the loaded Gorunfile, with answers worked out from it.
func (b *builder) gorunfileExamples(sc *dsScenario) {
	cfg := sc.cfg
	b.clearRuns(sc)
	servers := pipelines(cfg)
	ask := func(id, q, a string, want []string) {
		id = "file/" + sc.name + "/" + id
		b.add(dsExample{ID: id, Kind: "gorunfile", Scenario: sc.name, Want: want,
			Messages: b.chat(sc, b.withPrefix(q), "", a)}, id+q)
	}

	for _, s := range servers[:min(len(servers), 8)] {
		names := taskNamesOf(s)
		var a string
		switch {
		case len(names) > 0 && len(names) <= 15:
			a = fmt.Sprintf("%s runs %d tasks, in this order: %s.", s.Name, len(names), list(names))
		case len(names) > 15:
			a = fmt.Sprintf("%s runs %d tasks. The first 15, in run order, are %s. gorun Gorunfile -list-servers shows the whole list.", s.Name, len(names), strings.Join(names[:15], ", "))
		case s.IsLocal():
			a = fmt.Sprintf("%s runs no tasks. It has no SSH settings, so it runs on this machine, and only tasks marked local = true run there. Mark the tasks it should run local = true, or give it an ssh table.", s.Name)
		default:
			a = fmt.Sprintf("%s runs no tasks: its list is empty. Give it tasks with tasks or add_tasks, or add them to [servers] tasks or one of its groups.", s.Name)
		}
		want := []string{s.Name}
		if len(names) > 0 {
			want = append(want, names[0], names[len(names)-1])
		}
		for i, q := range []string{"Which tasks run on " + s.Name + "?", "what does " + s.Name + " run", "List the tasks for " + s.Name} {
			if b.rng.IntN(2) == 0 || i == 0 {
				ask("runs/"+s.Name, q, a, want)
			}
		}
		how := "over SSH, at " + s.Address()
		if s.IsLocal() {
			how = "on this machine: it has no SSH settings, so it is a local server"
		}
		ask("how/"+s.Name, one(b.rng, []string{"How does gorun reach " + s.Name + "?", "Does " + s.Name + " run over SSH?"}), s.Name+" runs "+how+".", []string{s.Name})
	}

	if len(servers) > 0 {
		var lines []string
		for _, s := range servers[:min(len(servers), 10)] {
			names := taskNamesOf(s)
			if len(names) > 15 {
				names = append(names[:15], fmt.Sprintf("and %d more", len(s.Tasks)-15))
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", s.Name, cmpOr(strings.Join(names, ", "), "no tasks")))
		}
		a := strings.Join(lines, "\n")
		if len(cfg.Servers) > len(lines) {
			a = fmt.Sprintf("There are %d servers; the first %d run:\n%s\n\ngorun Gorunfile -list-servers lists every server's tasks.", len(cfg.Servers), len(lines), a)
		}
		for _, q := range []string{"Which tasks run on which servers?", "what runs where?"} {
			ask("where", q, a, []string{servers[0].Name})
		}
		n := len(cfg.Servers)
		var a2 string
		if n <= 10 {
			names := make([]string, n)
			for i, s := range cfg.Servers {
				names[i] = s.Name
			}
			a2 = fmt.Sprintf("%d: %s.", n, list(names))
		} else {
			a2 = fmt.Sprintf("%d servers. Only the first %d are listed here; gorun Gorunfile -list-servers shows them all.", n, len(servers))
		}
		ask("count", one(b.rng, []string{"How many servers are there?", "How many servers does this Gorunfile have?"}), a2, []string{fmt.Sprint(n)})
	}

	names := slices.Sorted(maps.Keys(cfg.Tasks))
	names = names[:min(len(names), 40)]
	b.rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	for _, name := range names[:min(len(names), 6)] {
		t := cfg.Tasks[name]
		a := name + " doesn't depend on any task."
		want := []string{name}
		if len(t.DependsOn) > 0 {
			a = fmt.Sprintf("%s depends on %s, so %s first.", name, list(t.DependsOn), map[bool]string{true: "that runs", false: "those run"}[len(t.DependsOn) == 1])
			want = append(want, t.DependsOn...)
		}
		ask("deps/"+name, one(b.rng, []string{"What does " + name + " depend on?", "What has to run before " + name + "?"}), a, want)

		var users []string
		for _, other := range slices.Sorted(maps.Keys(cfg.Tasks))[:min(len(cfg.Tasks), 40)] {
			if slices.Contains(cfg.Tasks[other].DependsOn, name) {
				users = append(users, other)
			}
		}
		a = "No listed task depends on " + name + "."
		if len(users) > 0 {
			a = fmt.Sprintf("%s depend%s on %s, so if %s fails they are skipped.", list(users), map[bool]string{true: "s", false: ""}[len(users) == 1], name, name)
		}
		ask("users/"+name, one(b.rng, []string{"What depends on " + name + "?", "Which tasks need " + name + "?"}), a, append([]string{name}, users...))
	}

	// which servers run a task: each server's own list counts, not only its groups'
	for _, name := range names[:min(len(names), 8)] {
		var on []string
		for _, s := range servers {
			if slices.Contains(taskNamesOf(s), name) {
				on = append(on, s.Name)
			}
		}
		a := fmt.Sprintf("No server runs %s: it isn't in any server's task list, nor pulled in by one. Run it with gorun Gorunfile -r -task %s, or add it to a server's tasks.", name, name)
		want := []string{name}
		switch {
		case len(on) == len(servers) && len(on) > 1:
			a = fmt.Sprintf("Every server runs %s: %s.", name, list(on))
			want = append(want, on[0])
		case len(on) > 0:
			a = fmt.Sprintf("%s runs on %s.", name, list(on))
			want = append(want, on...)
		}
		ask("on/"+name, one(b.rng, []string{"Which servers run " + name + "?", "Where does " + name + " run?", "what servers is " + name + " on?"}), a, want[:min(len(want), 4)])
	}

	for _, g := range slices.Sorted(maps.Keys(cfg.Groups)) {
		var in []string
		for _, s := range servers {
			if slices.Contains(s.Groups, g) {
				in = append(in, s.Name)
			}
		}
		a := "No listed server is in the " + g + " group."
		if len(in) > 0 {
			a = fmt.Sprintf("The %s group has %s.", g, list(in))
		}
		ask("group/"+g, one(b.rng, []string{"Which servers are in the " + g + " group?", "who is in group " + g + "?"}), a, in[:min(len(in), 3)])
	}
	for _, bn := range slices.Sorted(maps.Keys(cfg.Bundles))[:min(len(cfg.Bundles), 5)] {
		ts := cfg.Bundles[bn].Tasks
		a := fmt.Sprintf("The %s bundle is %s. Use it as @%s in a task list, or run it with -task @%s.", bn, list(ts), bn, bn)
		ask("bundle/"+bn, one(b.rng, []string{"What's in the " + bn + " bundle?", "What does @" + bn + " run?"}), a, ts[:min(len(ts), 3)])
	}
}

func cmpOr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (b *builder) clearRuns(sc *dsScenario) {
	runs, _ := sc.app.store.ListRuns(b.ctx)
	for _, r := range runs {
		_ = sc.app.store.DeleteRun(b.ctx, r.ID)
	}
}

// simTask is one task of a simulated run.
type simTask struct {
	server, addr, name, cmd, stdout, stderr, verify string
	state, exit                                     int
}

type simRun struct {
	id, title    string
	tasks        []simTask
	servers      []gorun.Server
	serverErrors map[string]string
}

func (r simRun) count(state int) int {
	n := 0
	for _, t := range r.tasks {
		if t.state == state {
			n++
		}
	}
	return n
}

func runID(rng *rand.Rand) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", rng.Uint32(), rng.Uint32()&0xffff, rng.Uint32()&0xffff, rng.Uint32()&0xffff, rng.Uint64()&0xffffffffffff)
}

// simulate runs the scenario's SSH servers on paper: fail fails task on the servers in failOn,
// tasks that depend on a failed or skipped one are skipped, and the rest pass.
func (b *builder) simulate(sc *dsScenario, failOn []string, f *failure, task string) simRun {
	var servers, local []gorun.Server
	for _, s := range pipelines(sc.cfg) {
		switch {
		case len(s.Tasks) == 0:
		case s.IsLocal():
			local = append(local, s)
		default:
			servers = append(servers, s)
		}
	}
	if len(servers) == 0 {
		servers = local
	}
	if len(servers) > 4 {
		keep := servers[:0:0]
		for _, s := range servers {
			if slices.Contains(failOn, s.Name) {
				keep = append(keep, s)
			}
		}
		for _, s := range servers {
			if len(keep) >= 4 {
				break
			}
			if !slices.ContainsFunc(keep, func(k gorun.Server) bool { return k.Name == s.Name }) {
				keep = append(keep, s)
			}
		}
		servers = keep
	}
	r := simRun{id: runID(b.rng), title: cmpOr(sc.cfg.Title, "gorun"), servers: servers}
	for _, s := range servers {
		state := map[string]int{}
		for _, t := range s.Tasks {
			st := simTask{server: s.Name, addr: s.Address(), name: t.Name, cmd: t.Cmd, verify: t.Verify, state: store.StateDone}
			for _, d := range t.DependsOn {
				if ds, ok := state[d]; ok && (ds == store.StateFailed || ds == store.StateSkipped) {
					st.state = store.StateSkipped
					st.stderr = fmt.Sprintf("dependent task (%s) failed, aborting task (%s)", d, t.Name)
					break
				}
			}
			if st.state == store.StateDone {
				switch {
				case f != nil && t.Name == task && slices.Contains(failOn, s.Name):
					st.state, st.exit, st.stderr = store.StateFailed, f.Exit, b.fillFailure(f.Stderr, st)
				case (t.Check != "" || t.CheckCopies) && b.rng.IntN(5) < 2:
					st.state = store.StateUnchanged
				}
			}
			if st.state == store.StateDone {
				st.stdout = b.outputFor(t.Cmd).Stdout
			}
			state[t.Name] = st.state
			r.tasks = append(r.tasks, st)
		}
	}
	return r
}

var (
	installPkg = regexp.MustCompile(`install -y ([a-z0-9.+-]+)`)
	urlHost    = regexp.MustCompile(`https?://([a-z0-9.-]+)`)
	serviceArg = regexp.MustCompile(`systemctl (?:restart|reload|start|enable --now) ([a-z0-9@._-]+)`)
)

// fillFailure fills a failure's placeholders in for the task it fails.
func (b *builder) fillFailure(s string, t simTask) string {
	bin := strings.Fields(strings.TrimSpace(t.cmd) + " sh")[0]
	if bin == "cd" || strings.Contains(bin, "=") || bin == "if" || bin == "set" {
		bin = "sh"
	}
	pkg, host, svc := t.name, "deb.debian.org", t.name
	if m := installPkg.FindStringSubmatch(t.cmd); m != nil {
		pkg = m[1]
	}
	if m := urlHost.FindStringSubmatch(t.cmd); m != nil {
		host = m[1]
	}
	if m := serviceArg.FindStringSubmatch(t.cmd); m != nil {
		svc = m[1]
	}
	return fill(s, map[string]string{"bin": bin, "task": t.name, "server": t.server, "pkg": pkg, "host": host, "svc": svc, "verify": t.verify})
}

func (f failure) fits(t gorun.Task) bool {
	if t.Cmd == "" || (f.NeedsSudo && !t.Sudo) || (f.NoSudo && t.Sudo) || (f.HasVerify && t.Verify == "") {
		return false
	}
	if len(f.Match) == 0 {
		return true
	}
	return slices.ContainsFunc(f.Match, func(m string) bool { return strings.Contains(t.Cmd, m) })
}

// saveRun stores the run as the history would hold it.
func (b *builder) saveRun(sc *dsScenario, r simRun, at time.Time) {
	st, ctx := sc.app.store, b.ctx
	must := func(err error) {
		if err != nil {
			b.t.Fatal(err)
		}
	}
	must(st.CreateRun(ctx, r.id, r.title, "ops-laptop", 4000+b.rng.IntN(5000), at))
	for _, s := range r.servers {
		must(st.AddServer(ctx, r.id, s.Name, s.Address()))
	}
	for name, msg := range r.serverErrors {
		must(st.SetServerError(ctx, r.id, name, msg))
	}
	var total time.Duration
	for _, t := range r.tasks {
		d := time.Duration(200+b.rng.IntN(20000)) * time.Millisecond
		if t.state == store.StateSkipped || t.state == store.StateUnchanged {
			d /= 20
		}
		total += d
		must(st.UpsertTask(ctx, r.id, t.server, t.name, t.state, t.cmd, strings.TrimLeft(t.stdout, "\n"), t.stderr, t.exit, d))
		if t.state == store.StateFailed && strings.Contains(t.stderr, "verify failed") {
			must(st.SetTaskSteps(ctx, r.id, t.server, t.name, nil, &store.Step{Command: t.verify, ExitCode: 3, Stdout: "inactive"}))
		}
	}
	failed := r.count(store.StateFailed)
	ok := failed == 0 && len(r.serverErrors) == 0
	must(st.FinishRun(ctx, r.id, ok, len(r.tasks), failed, r.count(store.StateSkipped), total, at.Add(total)))
}

// olderRuns stores a few runs before at, which only the run list shows.
func (b *builder) olderRuns(sc *dsScenario, at time.Time) {
	for i := range b.rng.IntN(4) {
		r := b.simulate(sc, nil, nil, "")
		b.saveRun(sc, r, at.Add(-time.Duration(i+1)*time.Duration(3+b.rng.IntN(20))*time.Hour))
	}
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// skippedBy lists the tasks a failure skipped on server.
func skippedOn(r simRun, server string) []string {
	var out []string
	for _, t := range r.tasks {
		if t.server == server && t.state == store.StateSkipped {
			out = append(out, t.name)
		}
	}
	return out
}

func (b *builder) runExamples(sc *dsScenario, f failure) {
	// a task the failure fits, on a server that runs it
	type cand struct{ server, task string }
	var cands []cand
	for _, s := range pipelines(sc.cfg) {
		if s.IsLocal() {
			continue
		}
		for _, t := range s.Tasks {
			if f.fits(t) {
				cands = append(cands, cand{s.Name, t.Name})
			}
		}
	}
	if len(cands) == 0 {
		return
	}
	c := one(b.rng, cands)
	failOn := []string{c.server}
	if b.rng.IntN(4) == 0 {
		for _, o := range cands {
			if o.task == c.task && o.server != c.server {
				failOn = append(failOn, o.server)
				break
			}
		}
	}
	b.clearRuns(sc)
	now := time.Date(2026, 11, 20+b.rng.IntN(5), b.rng.IntN(24), b.rng.IntN(60), 0, 0, time.UTC)
	b.olderRuns(sc, now)
	r := b.simulate(sc, failOn, &f, c.task)
	b.saveRun(sc, r, now)

	// sometimes a newer run passed, and the question is about this one
	focus := ""
	if b.rng.IntN(4) == 0 {
		b.saveRun(sc, b.simulate(sc, nil, nil, ""), now.Add(2*time.Hour))
		focus = r.id
	}

	var ft simTask
	for _, t := range r.tasks {
		if t.state == store.StateFailed {
			ft = t
			break
		}
	}
	quote := b.fillFailure(f.Quote, ft)
	cause := b.fillFailure(f.Cause, ft)
	fix := b.fillFailure(f.Fix, ft)
	where := list(failOn)
	skipped := skippedOn(r, failOn[0])
	var others []string
	for _, s := range r.servers {
		if !slices.Contains(failOn, s.Name) {
			others = append(others, s.Name)
		}
	}
	tail := ""
	if len(skipped) > 0 {
		verb := "were"
		if len(skipped) == 1 {
			verb = "was"
		}
		tail += fmt.Sprintf(" Because it failed, %s %s skipped on %s; %s once %s passes.", list(skipped), verb, where, map[bool]string{true: "it runs", false: "they run"}[len(skipped) == 1], c.task)
	}
	if len(others) > 0 {
		tail += fmt.Sprintf(" Nothing failed on %s.", list(others))
	}
	answers := []string{
		fmt.Sprintf("%s failed on %s with exit code %d. Its output says:\n\n```\n%s\n```\n\nThat means %s.\n\nTo fix it, %s.%s", c.task, where, ft.exit, quote, cause, fix, tail),
		fmt.Sprintf("The run failed because %s failed on %s (exit code %d): %s. The line that shows it:\n\n```\n%s\n```\n\nTo fix it, %s.%s", c.task, where, ft.exit, cause, quote, fix, tail),
		fmt.Sprintf("%s on %s failed, exit code %d.\n\n```\n%s\n```\n\nCause: %s.\nFix: %s.%s", c.task, where, ft.exit, quote, upper(cause), upper(fix), tail),
	}
	want := []string{c.task, keyWord(quote)}
	qs := []string{"Why did the last run fail?", "What went wrong?", "Why did the run fail, and how do I fix it?", "why did " + c.task + " fail?", c.task + " failed on " + c.server + ", what's wrong?", "Diagnose the last run"}
	if focus != "" {
		qs = []string{"Why did this run fail, and how do I fix it?", "Why did run " + r.id + " fail?", "What failed in this run?"}
	}
	id := fmt.Sprintf("run/%s/%s/%s", sc.name, f.ID, c.task)
	q := one(b.rng, qs)
	b.add(dsExample{ID: id, Kind: "diagnose", Scenario: sc.name, Want: want,
		Messages: b.chat(sc, b.withPrefix(q), focus, one(b.rng, answers))}, id+q)

	// why a task was skipped
	if len(skipped) > 0 {
		sk := one(b.rng, skipped)
		dep, depState := "", 0
		for _, d := range sc.cfg.Tasks[sk].DependsOn {
			for _, t := range r.tasks {
				if t.server == failOn[0] && t.name == d && (t.state == store.StateFailed || t.state == store.StateSkipped) {
					dep, depState = d, t.state
				}
			}
		}
		if dep != "" {
			var a string
			if depState == store.StateFailed {
				a = fmt.Sprintf("%s was skipped on %s because it depends on %s, which failed with exit code %d: %s. Fix %s and %s runs on the next run.", sk, failOn[0], dep, ft.exit, cause, dep, sk)
			} else {
				a = fmt.Sprintf("%s was skipped on %s because it depends on %s, which was skipped too, because %s failed earlier (exit code %d): %s. Fix %s and the tasks after it run again.", sk, failOn[0], dep, c.task, ft.exit, cause, c.task)
			}
			q := one(b.rng, []string{"Why was " + sk + " skipped?", "why didn't " + sk + " run?", sk + " says skipped, why?"})
			b.add(dsExample{ID: id + "/skip", Kind: "diagnose", Scenario: sc.name, Want: []string{sk, dep},
				Messages: b.chat(sc, b.withPrefix(q), focus, a)}, id+"/skip"+q)
		}
	}

	// a summary of the run
	var sum strings.Builder
	fmt.Fprintf(&sum, "Run %s (%s) failed. It ran %d tasks on %d servers: %d failed, %d skipped", r.id, r.title, len(r.tasks), len(r.servers), r.count(store.StateFailed), r.count(store.StateSkipped))
	if u := r.count(store.StateUnchanged); u > 0 {
		fmt.Fprintf(&sum, ", %d unchanged because their checks found nothing to do", u)
	}
	fmt.Fprintf(&sum, ".\n\n- %s failed on %s (exit code %d): %s.", c.task, where, ft.exit, cause)
	if len(skipped) > 0 {
		fmt.Fprintf(&sum, "\n- Skipped because of it: %s.", list(skipped))
	}
	sum.WriteString("\n- Every other task passed.")
	sq := "Summarise the last run"
	if focus != "" {
		sq = "Summarise this run"
	}
	b.add(dsExample{ID: id + "/summary", Kind: "summary", Scenario: sc.name, Want: []string{c.task, "failed"},
		Messages: b.chat(sc, b.withPrefix(sq), focus, sum.String())}, id+"/summary")

	if focus == "" && b.rng.IntN(2) == 0 {
		b.recentRunsExample(sc, id)
	}
}

// keyWord is the longest word of s, which a good answer quoting the output contains.
func keyWord(s string) string {
	best := ""
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return strings.ContainsRune(" \t\n\"'():,", r) }) {
		w = strings.TrimRight(w, ".")
		if len(w) > len(best) {
			best = w
		}
	}
	return best
}

// recentRunsExample summarises the run list.
func (b *builder) recentRunsExample(sc *dsScenario, id string) {
	runs, err := sc.app.runList(b.ctx)
	if err != nil || len(runs) < 2 {
		return
	}
	var a strings.Builder
	fmt.Fprintf(&a, "The %d most recent runs, newest first:\n", min(len(runs), 10))
	failed := 0
	for _, r := range runs[:min(len(runs), 10)] {
		status := "passed"
		if !r.Success {
			status = fmt.Sprintf("failed (%d failed, %d skipped)", r.Failed, r.Skipped)
			failed++
		}
		fmt.Fprintf(&a, "- %s, %s: %s, %d tasks\n", r.StartedAt.Format(time.DateTime), r.ID[:8], status, r.TotalTasks)
	}
	if failed == 0 {
		a.WriteString("\nAll of them passed.")
	} else {
		fmt.Fprintf(&a, "\n%d of them failed. Ask about one to see why.", failed)
	}
	q := one(b.rng, []string{"Summarise the recent runs", "How have the recent runs gone?", "show me the latest runs"})
	b.add(dsExample{ID: id + "/recent", Kind: "summary", Scenario: sc.name, Want: []string{runs[0].ID[:8]},
		Messages: b.chat(sc, b.withPrefix(q), "", strings.TrimSpace(a.String()))}, id+"/recent")
}

func (b *builder) serverErrorExamples(sc *dsScenario) {
	r := b.simulate(sc, nil, nil, "")
	if len(r.servers) == 0 {
		return
	}
	s := one(b.rng, r.servers)
	host := strings.Split(s.Address(), ":")[0]
	type sshErr struct{ msg, cause, fix string }
	errs := []sshErr{
		{"dial tcp " + s.Address() + ": connect: connection refused", "nothing accepted the SSH connection at " + s.Address() + ": sshd isn't running there, or the port is wrong", "check sshd is running on " + s.Name + " and the port in its ssh settings"},
		{"ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain", s.Name + " rejected the SSH login: the username or key is wrong", "check the username and identityfile for " + s.Name + " (in its ssh table, its group's or your ~/.ssh/config), and that the public key is in authorized_keys there"},
		{"dial tcp " + s.Address() + ": i/o timeout", s.Name + " didn't answer at " + s.Address() + ": it is down, or a firewall drops SSH", "check that " + s.Name + " is up and that port " + strings.Split(s.Address(), ":")[1] + " is open from this machine"},
		{"dial tcp: lookup " + host + ": no such host", host + " doesn't resolve, so gorun couldn't find " + s.Name, "check the hostname in the Gorunfile, or add it to DNS or /etc/hosts"},
	}
	e := one(b.rng, errs)
	r.tasks = slices.DeleteFunc(r.tasks, func(t simTask) bool { return t.server == s.Name })
	r.serverErrors = map[string]string{s.Name: e.msg}
	b.clearRuns(sc)
	now := time.Date(2026, 11, 22, b.rng.IntN(24), 0, 0, 0, time.UTC)
	b.olderRuns(sc, now)
	b.saveRun(sc, r, now)
	others := ""
	var names []string
	for _, o := range r.servers {
		if o.Name != s.Name {
			names = append(names, o.Name)
		}
	}
	if len(names) > 0 {
		others = " The other servers (" + list(names) + ") ran their tasks."
		if r.count(store.StateFailed) > 0 {
			others = ""
		}
	}
	a := fmt.Sprintf("%s never ran its tasks: gorun couldn't connect to it over SSH.\n\n```\n%s\n```\n\nThat means %s. To fix it, %s.%s", s.Name, e.msg, e.cause, e.fix, others)
	q := one(b.rng, []string{"Why did the last run fail?", "Why didn't " + s.Name + " run anything?", "What went wrong?"})
	id := "server/" + sc.name + "/" + s.Name + "/" + keyWord(e.msg)
	b.add(dsExample{ID: id, Kind: "diagnose", Scenario: sc.name, Want: []string{s.Name, "ssh"},
		Messages: b.chat(sc, b.withPrefix(q), "", a)}, id+q)
}

func (b *builder) passedRunExamples(sc *dsScenario) {
	b.clearRuns(sc)
	now := time.Date(2026, 11, 23, b.rng.IntN(24), 0, 0, 0, time.UTC)
	b.olderRuns(sc, now)
	r := b.simulate(sc, nil, nil, "")
	if len(r.tasks) == 0 {
		return
	}
	b.saveRun(sc, r, now)
	a := fmt.Sprintf("Yes. Run %s (%s) passed: %d tasks on %d servers, none failed.", r.id, r.title, len(r.tasks), len(r.servers))
	if u := r.count(store.StateUnchanged); u > 0 {
		a += fmt.Sprintf(" %d of them were unchanged: their checks found nothing to do, which counts as a success.", u)
	}
	q := one(b.rng, []string{"Did the last run pass?", "Did the latest run work?", "Summarise the last run", "Did anything fail?"})
	if strings.HasPrefix(q, "Summarise") {
		a = strings.TrimPrefix(a, "Yes. ")
	}
	if q == "Did anything fail?" {
		a = "No. " + strings.TrimPrefix(a, "Yes. ")
	}
	id := "passed/" + sc.name + "/" + r.id[:8]
	b.add(dsExample{ID: id, Kind: "summary", Scenario: sc.name, Want: []string{"passed"},
		Messages: b.chat(sc, b.withPrefix(q), "", a)}, id)
}

func writeJSONL(t *testing.T, path string, exs []dsExample) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, ex := range exs {
		if err := enc.Encode(ex); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEvalModel asks the model in GORUN_EVAL_MODEL (a GGUF path, or "base" for the built-in
// one) every held-out example, and scores the replies.
func TestEvalModel(t *testing.T) {
	model := os.Getenv("GORUN_EVAL_MODEL")
	if model == "" {
		t.Skip("set GORUN_EVAL_MODEL to a GGUF path, or base")
	}
	label := strings.TrimSuffix(filepath.Base(model), ".gguf")
	if model == "base" {
		model = ""
	}
	e := &llm.Engine{Dir: llm.Dir(), Model: model, Progress: os.Stderr}
	defer e.Close()

	f, err := os.Open(filepath.Join(dataDir, "eval.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var exs []dsExample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var ex dsExample
		if err := json.Unmarshal(sc.Bytes(), &ex); err != nil {
			t.Fatal(err)
		}
		exs = append(exs, ex)
	}
	_ = f.Close()
	// GORUN_EVAL_N asks about every k-th example, so each kind keeps its share
	if n, _ := strconv.Atoi(os.Getenv("GORUN_EVAL_N")); n > 0 && n < len(exs) {
		slices.SortStableFunc(exs, func(a, b dsExample) int { return strings.Compare(a.Kind, b.Kind) })
		step := float64(len(exs)) / float64(n)
		var keep []dsExample
		for i := range n {
			keep = append(keep, exs[int(float64(i)*step)])
		}
		exs = keep
	}

	cfgs := map[string]*gorun.Config{}
	config := func(name string) *gorun.Config {
		if name == "" {
			return nil
		}
		if c, ok := cfgs[name]; ok {
			return c
		}
		c := &gorun.Config{}
		if err := c.LoadFile(filepath.Join(repoRoot, name)); err != nil {
			t.Fatal(err)
		}
		cfgs[name] = c
		return c
	}

	_ = os.MkdirAll(outDir, 0o755)
	out, err := os.Create(filepath.Join(outDir, "eval-"+label+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	enc := json.NewEncoder(out)

	pass, total := map[string]int{}, map[string]int{}
	start := time.Now()
	for i, ex := range exs {
		reply, err := e.Chat(context.Background(), ex.Messages[:len(ex.Messages)-1])
		if err != nil {
			t.Fatalf("%s: %v", ex.ID, err)
		}
		ok, why := score(ex, reply, config(ex.Scenario))
		total[ex.Kind]++
		if ok {
			pass[ex.Kind]++
		}
		_ = enc.Encode(map[string]any{"id": ex.ID, "kind": ex.Kind, "ok": ok, "why": why, "reply": reply, "want": ex.Messages[len(ex.Messages)-1].Content})
		if (i+1)%20 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d in %s\n", i+1, len(exs), time.Since(start).Round(time.Second))
		}
	}
	var all, allPass int
	for _, k := range slices.Sorted(maps.Keys(total)) {
		fmt.Printf("%-10s %3d/%3d  %5.1f%%\n", k, pass[k], total[k], 100*float64(pass[k])/float64(total[k]))
		all += total[k]
		allPass += pass[k]
	}
	fmt.Printf("%-10s %3d/%3d  %5.1f%%  (%s, %s)\n", "all", allPass, all, 100*float64(allPass)/float64(all), label, time.Since(start).Round(time.Second))
}

// score reports whether reply does what the example asks: tasks that load where tasks are
// wanted, and every wanted word.
func score(ex dsExample, reply string, cfg *gorun.Config) (bool, string) {
	if ex.Tasks {
		if ex.Kind == "chat-gen" && !strings.Contains(reply, "```") {
			return false, "no code block"
		}
		extract := gorun.ExtractTasks
		if ex.Kind == "chat-edit" {
			// an edit replaces a task, as the chat's Save does
			extract = gorun.ExtractTaskEdits
		}
		text, err := extract(reply, cfg)
		if err != nil {
			return false, err.Error()
		}
		if why := badCommand(text); why != "" {
			return false, why
		}
		if why := taskShape(text, ex.Messages[len(ex.Messages)-1].Content, cfg); why != "" {
			return false, why
		}
		if why := scheduleShape(text, ex.Messages[len(ex.Messages)-1].Content, cfg); why != "" {
			return false, why
		}
	}
	low := strings.ToLower(reply)
	if ex.Kind == "chat-edit" {
		// the wanted keys are as Marshal writes them, so "5m" counts as "5m0s"
		got := decodeTasks(reply)
		for name, t := range got {
			t.Name = name
			got[name] = t
		}
		data, err := (&gorun.Config{Tasks: got}).Marshal()
		if err != nil {
			return false, err.Error()
		}
		low = strings.ToLower(string(data))
	}
	for _, w := range ex.Want {
		if !strings.Contains(low, strings.ToLower(w)) {
			return false, "missing " + w
		}
	}
	return true, ""
}

// taskShape reports tasks that break TaskFormat's other rules: one task where the example splits
// the job into small ones, or a task not marked local in a Gorunfile with no SSH server.
func taskShape(text, want string, cfg *gorun.Config) string {
	got, wanted := decodeTasks(text), decodeTasks(want)
	if len(wanted) > 1 && len(got) < 2 {
		return fmt.Sprintf("one task where %d small ones are wanted", len(wanted))
	}
	if cfg == nil || hasSSH(cfg) {
		return ""
	}
	for name, t := range got {
		if !t.Local {
			return name + " isn't local = true, and the Gorunfile has no SSH server"
		}
	}
	return ""
}

var taskBlock = regexp.MustCompile("(?s)```(?:toml)?\\n(.*?\\[tasks\\..*?)```")

// decodeTasks returns the tasks in text, or in its first code block holding them.
func decodeTasks(text string) map[string]gorun.Task {
	if m := taskBlock.FindStringSubmatch(text); m != nil {
		text = m[1]
	}
	var doc struct {
		Tasks map[string]gorun.Task `toml:"tasks"`
	}
	_, _ = toml.Decode(text, &doc)
	return doc.Tasks
}

// badCommand reports a command that breaks TaskFormat's rules though it loads: sudo written in
// it (gorun adds sudo itself), or a stray pipe at its start.
func badCommand(text string) string {
	var doc struct {
		Tasks map[string]gorun.Task `toml:"tasks"`
	}
	if _, err := toml.Decode(text, &doc); err != nil {
		return err.Error()
	}
	for name, t := range doc.Tasks {
		for _, c := range []string{t.Cmd, t.Check, t.Verify} {
			c = strings.TrimSpace(c)
			switch {
			case strings.HasPrefix(c, "sudo ") || strings.Contains(c, " sudo ") || strings.Contains(c, "&& sudo") || strings.Contains(c, "| sudo"):
				return name + " writes sudo in a command"
			case strings.HasPrefix(c, "|"):
				return name + " starts a command with a pipe"
			}
		}
	}
	return ""
}

// when is a timing a request can ask for, the cron or at it means, and a schedule name suffix.
type when struct{ say, cron, at, suffix string }

var whens = []when{
	{"every night at 2am", "0 2 * * *", "", "nightly"},
	{"every day at 6:30", "30 6 * * *", "", "daily"},
	{"every Monday at 9am", "0 9 * * 1", "", "weekly"},
	{"every 15 minutes", "*/15 * * * *", "", "every-15m"},
	{"every hour", "0 * * * *", "", "hourly"},
	{"on weekdays at 7pm", "0 19 * * 1-5", "", "weekdays"},
	{"on the first of every month at midnight", "0 0 1 * *", "", "monthly"},
	{"every Sunday at 3am", "0 3 * * 0", "", "sunday"},
	{"every 6 hours", "0 */6 * * *", "", "every-6h"},
	{"once, on 15 January 2027 at 10am", "", "2027-01-15T10:00", "once"},
	{"at 11:45 every night", "45 23 * * *", "", "late"},
}

// zones are the time zones a request can name, with how it says them.
var zones = [][2]string{{"Sydney time", "Australia/Sydney"}, {"New York time", "America/New_York"}, {"Berlin time", "Europe/Berlin"}, {"Tokyo time", "Asia/Tokyo"}}

// scheduleTOML writes s as a reply writes it: every key it has, in a fixed order.
func scheduleTOML(s gorun.Schedule) string {
	var b strings.Builder
	b.WriteString("[[schedule]]\n")
	fmt.Fprintf(&b, "name = %q\n", s.Name)
	if s.Description != "" {
		fmt.Fprintf(&b, "description = %q\n", s.Description)
	}
	fmt.Fprintf(&b, "target = %q\n", s.Target)
	if s.Cron != "" {
		fmt.Fprintf(&b, "cron = %q\n", s.Cron)
	} else {
		fmt.Fprintf(&b, "at = %q\n", s.At)
	}
	if s.Timezone != "" {
		fmt.Fprintf(&b, "timezone = %q\n", s.Timezone)
	}
	if s.Missed != "" {
		fmt.Fprintf(&b, "missed = %q\n", s.Missed)
	}
	return strings.TrimSpace(b.String())
}

// timing fills in s from w, with a zone and a catch-up the request may add, and returns the
// words the request adds for them.
func (b *builder) timing(s *gorun.Schedule, w when) string {
	s.Cron, s.At = w.cron, w.at
	say := " " + w.say
	if b.rng.IntN(3) == 0 {
		z := one(b.rng, zones)
		s.Timezone = z[1]
		say += ", " + z[0]
	}
	if w.cron != "" && b.rng.IntN(5) == 0 {
		s.Missed = gorun.MissedRun
		say += ", and catch up if gorun was down"
	}
	return say
}

func scheduleNote(s gorun.Schedule, what string) string {
	when := "on cron " + s.Cron
	if s.At != "" {
		when = "once, at " + s.At
	}
	n := fmt.Sprintf("This schedule runs %s %s", what, when)
	if s.Timezone != "" {
		n += " in " + s.Timezone
	}
	n += "."
	if s.Missed == gorun.MissedRun {
		n += " missed = \"run\" runs it once on start-up when a time was missed."
	}
	return n + " gorun -serve runs schedules, so keep it running."
}

// scheduleExamples ask for new schedules of sc's tasks and bundles, and for changes to the
// schedules it has.
func (b *builder) scheduleExamples(sc *dsScenario) {
	cfg := sc.cfg
	targets := listedTasks(cfg, !hasSSH(cfg))
	for _, bn := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		targets = append(targets, gorun.BundlePrefix+bn)
	}
	if len(targets) == 0 {
		return
	}
	asks := []string{"Run %s%s", "Schedule %s to run%s", "can you set up a schedule that runs %s%s?", "I want %s to run%s", "add a schedule: %s%s"}
	for i := range 14 {
		target := one(b.rng, targets)
		w := one(b.rng, whens)
		s := gorun.Schedule{Name: strings.TrimPrefix(target, gorun.BundlePrefix) + "-" + w.suffix, Target: target}
		what := target
		if bn, ok := strings.CutPrefix(target, gorun.BundlePrefix); ok {
			what = "the " + bn + " bundle"
		}
		say := b.timing(&s, w)
		q := fmt.Sprintf(one(b.rng, asks), what, say)
		b.scheduleChat(sc, fmt.Sprintf("sched/%s/add/%d", sc.name, i), q, scheduleNote(s, what), scheduleTOML(s))
	}
	for i, old := range cfg.Schedules[:min(len(cfg.Schedules), 4)] {
		for j := range 3 {
			s := old
			var q, note string
			switch b.rng.IntN(4) {
			case 0:
				w := one(b.rng, whens)
				s.Timezone, s.Missed = old.Timezone, old.Missed
				s.Cron, s.At = w.cron, w.at
				q = fmt.Sprintf(one(b.rng, []string{"Change %s to run %s", "move the %s schedule to %s", "make %s run %s instead"}), old.Name, w.say)
				note = "This replaces " + old.Name + " with the new timing; the rest stays as it was."
			case 1:
				z := one(b.rng, zones)
				s.Timezone = z[1]
				q = fmt.Sprintf(one(b.rng, []string{"Make %s use %s", "%s should be in %s"}), old.Name, z[0])
				note = "This replaces " + old.Name + ": its times are now read in " + z[1] + "."
			case 2:
				if old.Cron == "" {
					continue
				}
				s.Missed = gorun.MissedRun
				q = fmt.Sprintf(one(b.rng, []string{"Make %s catch up runs it missed", "if gorun is down when %s is due, run it when it starts"}), old.Name)
				note = "This replaces " + old.Name + " with missed = \"run\": when gorun -serve was down at a due time, it runs once on start-up."
			default:
				target := one(b.rng, targets)
				if target == old.Target {
					continue
				}
				s.Target = target
				q = fmt.Sprintf(one(b.rng, []string{"Point %s at %s", "make the %s schedule run %s"}), old.Name, target)
				note = "This replaces " + old.Name + ": it now runs " + target + " at the same times."
			}
			b.scheduleChat(sc, fmt.Sprintf("sched/%s/edit/%d/%d", sc.name, i, j), q, note, scheduleTOML(s))
		}
	}
}

// scheduleChat adds a chat example whose reply is note and answer, when answer loads in sc.
func (b *builder) scheduleChat(sc *dsScenario, id, q, note, answer string) {
	if _, err := gorun.ExtractTaskEdits("```toml\n"+answer+"\n```", sc.cfg); err != nil {
		return
	}
	b.add(dsExample{ID: id, Kind: "chat-sched", Scenario: sc.name, Tasks: true,
		Messages: b.chat(sc, b.withPrefix(q), "", chatTasks(note, answer))}, id)
}

// withSchedule adds a schedule for the last task in answer, the one that finishes the job.
func (b *builder) withSchedule(answer string) (string, string, gorun.Schedule) {
	heads := taskHeader.FindAllString(answer, -1)
	last := strings.TrimSuffix(strings.TrimPrefix(heads[len(heads)-1], "[tasks."), "]")
	w := one(b.rng, whens[:9])
	s := gorun.Schedule{Name: last + "-" + w.suffix, Target: last}
	say := b.timing(&s, w)
	return answer + "\n\n" + scheduleTOML(s), say, s
}

// scheduleShape reports a reply whose schedules don't match the example's: each wanted one needs
// one with its target and timing, and its name when it changes a schedule cfg has.
func scheduleShape(text, want string, cfg *gorun.Config) string {
	var got, wanted struct {
		Schedule []gorun.Schedule `toml:"schedule"`
	}
	_, _ = toml.Decode(stripBlock(text), &got)
	_, _ = toml.Decode(stripBlock(want), &wanted)
	for _, w := range wanted.Schedule {
		edit := cfg != nil && slices.ContainsFunc(cfg.Schedules, func(s gorun.Schedule) bool { return s.Name == w.Name })
		if !slices.ContainsFunc(got.Schedule, func(g gorun.Schedule) bool {
			return g.Target == w.Target && g.Cron == w.Cron && g.At == w.At && g.Timezone == w.Timezone && g.Missed == w.Missed && (!edit || g.Name == w.Name)
		}) {
			return fmt.Sprintf("no schedule runs %s on %q %s as wanted", w.Target, w.Cron+w.At, w.Timezone)
		}
	}
	return ""
}

// stripBlock is the TOML of a reply's code block, or the reply itself.
func stripBlock(s string) string {
	if m := regexp.MustCompile("(?s)```(?:toml)?\\n(.*?)```").FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// taskEditExamples ask for a change to a task the Gorunfile has: a timeout, retries, a
// dependency or sudo. The answer is the whole task again, as the chat shows it to the model, with
// only that changed.
func (b *builder) taskEditExamples(sc *dsScenario) {
	cfg := sc.cfg
	b.clearRuns(sc)
	names := slices.Sorted(maps.Keys(cfg.Tasks))
	b.rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	for i, name := range append(names[:min(len(names), 16)], names[:min(len(names), 16)]...) {
		t := cfg.Tasks[name]
		var q, note string
		var want []string
		switch (i + b.rng.IntN(4)) % 4 {
		case 0:
			mins := one(b.rng, []int{5, 10, 20, 30, 45, 60, 90})
			if t.Timeout == time.Duration(mins)*time.Minute {
				continue
			}
			t.Timeout = time.Duration(mins) * time.Minute
			q = fmt.Sprintf(one(b.rng, []string{"Add a timeout of %d minutes to %s", "Give %[2]s a %[1]d minute timeout", "%[2]s should give up after %[1]d minutes", "set the timeout on %[2]s to %[1]d minutes"}), mins, name)
			note = fmt.Sprintf("This sets timeout = \"%dm\" on %s; the rest stays as it was.", mins, name)
			want = []string{fmt.Sprintf("timeout = \"%s", durationText(t.Timeout))}
		case 1:
			n := one(b.rng, []int{2, 3, 5})
			delay := one(b.rng, []time.Duration{10 * time.Second, 30 * time.Second, time.Minute})
			if t.Retries == n {
				continue
			}
			t.Retries, t.RetryDelay = n, &delay
			q = fmt.Sprintf(one(b.rng, []string{"Retry %s %d times, %s apart", "make %[1]s retry %[2]d times with %[3]s between tries", "%[1]s fails now and then; let it try %[2]d more times, waiting %[3]s"}), name, n, durationText(delay))
			note = fmt.Sprintf("This gives %s retries = %d with retry_delay = \"%s\"; the rest stays as it was.", name, n, durationText(delay))
			want = []string{fmt.Sprintf("retries = %d", n)}
		case 2:
			var cands []string
			for _, other := range slices.Sorted(maps.Keys(cfg.Tasks)) {
				if other != name && !slices.Contains(t.DependsOn, other) && !dependsOn(cfg, other, name) {
					cands = append(cands, other)
				}
			}
			if len(cands) == 0 {
				continue
			}
			dep := one(b.rng, cands)
			t.DependsOn = append(slices.Clone(t.DependsOn), dep)
			q = fmt.Sprintf(one(b.rng, []string{"Make %s run after %s", "%[1]s should wait for %[2]s", "%[1]s needs %[2]s to have run first"}), name, dep)
			note = fmt.Sprintf("This adds %s to %s's depends_on, so it runs after it and is skipped when it fails; the rest stays as it was.", dep, name)
			want = []string{dep}
		default:
			if t.Sudo {
				continue
			}
			t.Sudo = true
			q = fmt.Sprintf(one(b.rng, []string{"%s needs root", "run %s with sudo", "%[1]s fails with permission denied, make it run as root"}), name)
			note = fmt.Sprintf("This sets sudo = true on %s, so gorun runs it as root with sudo -n; the rest stays as it was.", name)
			want = []string{"sudo = true"}
		}
		data, err := (&gorun.Config{Tasks: map[string]gorun.Task{name: t}}).Marshal()
		if err != nil {
			b.t.Fatalf("edit %s: %v", name, err)
		}
		answer := strings.TrimSpace(string(data))
		if _, err := gorun.ExtractTaskEdits(answer, cfg); err != nil {
			continue
		}
		id := fmt.Sprintf("edit/%s/%s", sc.name, name)
		b.add(dsExample{ID: id, Kind: "chat-edit", Scenario: sc.name, Tasks: true, Want: append([]string{name}, want...),
			Messages: b.chat(sc, b.withPrefix(q), "", chatTasks(note, answer))}, id+q)
	}
}

// dependsOn reports whether task a depends on b, directly or through others.
func dependsOn(cfg *gorun.Config, a, b string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(n string) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		for _, d := range cfg.Tasks[n].DependsOn {
			if d == b || walk(d) {
				return true
			}
		}
		return false
	}
	return walk(a)
}

// durationText is a duration as Marshal writes it, such as 30m0s.
func durationText(d time.Duration) string { return d.String() }

// outputFor is what cmd prints when it is one of the catalogue's read-only commands, writing to
// stdout, or else nothing.
func (b *builder) outputFor(cmd string) output {
	cmd = strings.TrimSpace(cmd)
	if strings.ContainsAny(cmd, ">|\n") {
		return output{}
	}
	for _, o := range b.outputs {
		for _, m := range o.Match {
			if cmd == m || strings.HasPrefix(cmd, m+" ") {
				return o
			}
		}
	}
	return output{}
}

// runChatRounds is how many runs runChatExamples asks about: more where tasks print output.
func (b *builder) runChatRounds(sc *dsScenario) int {
	for _, t := range sc.cfg.Tasks {
		if b.outputFor(t.Cmd).Stdout != "" {
			return 10
		}
	}
	return 3
}

// bullets lists lines as a markdown list.
func bullets(lines []string) string { return "- " + strings.Join(lines, "\n- ") }

// runChatExamples ask about a passed run on one server: its counts, which tasks were unchanged
// and why, the list sorted, a wrong count put right, why tasks ran, and what they printed. The
// answers come from the run's record, which the context counts and lists.
func (b *builder) runChatExamples(sc *dsScenario) {
	r := b.simulate(sc, nil, nil, "")
	if len(r.tasks) == 0 {
		return
	}
	srv := one(b.rng, r.servers)
	r.servers = []gorun.Server{srv}
	r.tasks = slices.DeleteFunc(r.tasks, func(t simTask) bool { return t.server != srv.Name })
	// a third of the time the run is one task that printed, like a schedule that runs one
	if b.rng.IntN(3) == 0 {
		if p := slices.DeleteFunc(slices.Clone(r.tasks), func(t simTask) bool { return t.stdout == "" }); len(p) > 0 {
			r.tasks = []simTask{one(b.rng, p)}
		}
	}
	if len(r.tasks) > maxListedTasks {
		return
	}
	b.clearRuns(sc)
	now := time.Date(2026, 11, 24+b.rng.IntN(4), b.rng.IntN(24), b.rng.IntN(60), 0, 0, time.UTC)
	b.olderRuns(sc, now)
	b.saveRun(sc, r, now)
	focus, this := "", "the last run"
	if b.rng.IntN(2) == 0 {
		b.saveRun(sc, b.simulate(sc, nil, nil, ""), now.Add(time.Hour))
		focus, this = r.id, "this run"
	}
	var ran, unchanged []simTask
	for _, t := range r.tasks {
		switch t.state {
		case store.StateDone:
			ran = append(ran, t)
		case store.StateUnchanged:
			unchanged = append(unchanged, t)
		}
	}
	base := "runchat/" + sc.name + "/" + r.id[:8]
	ask := func(id, kind string, conv []gorun.ChatMessage, a string, want []string) {
		id = base + "/" + id
		b.add(dsExample{ID: id, Kind: kind, Scenario: sc.name, Want: want,
			Messages: b.chat(sc, conv, focus, a)}, id)
	}
	turn := func(conv []gorun.ChatMessage, a, q string) []gorun.ChatMessage {
		return append(slices.Clone(conv), gorun.ChatMessage{Role: "assistant", Content: a}, gorun.ChatMessage{Role: "user", Content: q})
	}
	user := func(q string) []gorun.ChatMessage { return []gorun.ChatMessage{{Role: "user", Content: q}} }

	// the summary, with the counts as the record has them
	summary := fmt.Sprintf("Run %s (%s) passed: %d tasks on %s, none failed.", r.id, r.title, len(r.tasks), srv.Name)
	switch {
	case len(unchanged) == 0:
		summary += " Every task ran."
	case len(ran) == 0:
		summary += " All of them were unchanged: their checks found nothing to do, which counts as a success."
	default:
		summary += fmt.Sprintf(" %d ran and %d %s unchanged: their checks found nothing to do, which counts as a success.", len(ran), len(unchanged), map[bool]string{true: "was", false: "were"}[len(unchanged) == 1])
	}
	sq := one(b.rng, []string{"Summarise " + this, "How did " + this + " go?", "summarise " + this + " for me"})
	sumConv := user(sq)
	ask("summary", "summary", sumConv, summary, []string{"passed", strconv.Itoa(len(r.tasks))})

	// which were unchanged, and why: each one's check
	why := func(ts []simTask) []string {
		var out []string
		for _, t := range ts {
			c := sc.cfg.Tasks[t.name]
			switch {
			case c.Check != "":
				out = append(out, fmt.Sprintf("%s: its check, `%s`, exited 0", t.name, c.Check))
			case c.CheckCopies:
				out = append(out, t.name+": its files were already there, as check_copies checks")
			default:
				out = append(out, t.name)
			}
		}
		return out
	}
	if len(unchanged) > 0 {
		lines := why(unchanged)
		uq := one(b.rng, []string{"Which tasks were unchanged, and why?", "which checks were unchanged? show me why for each of them", "Which tasks didn't need to run?"})
		ua := fmt.Sprintf("%d tasks were unchanged on %s. A check exits 0 when there is nothing to do, so the task's command didn't run:\n\n%s\n\nThe other %d ran.", len(unchanged), srv.Name, bullets(lines), len(ran))
		if len(unchanged) == 1 {
			ua = fmt.Sprintf("1 task was unchanged on %s. Its check exits 0 when there is nothing to do, so its command didn't run:\n\n%s\n\nThe other %d ran.", srv.Name, bullets(lines), len(ran))
		}
		ask("unchanged", "summary", user(uq), ua, []string{unchanged[0].name})

		if len(unchanged) > 2 {
			sorted := slices.Clone(lines)
			slices.Sort(sorted)
			if !slices.Equal(sorted, lines) {
				oq := one(b.rng, []string{"order the list alphabetically", "sort them by name", "can you sort that list alphabetically?"})
				ask("sorted", "summary", turn(user(uq), ua, oq), "Sorted by name:\n\n"+bullets(sorted), []string{strings.SplitN(sorted[0], ":", 2)[0]})
			}
		}

		// the user remembers another count: the record decides, and says what ran
		wrong := len(unchanged) + 1 + b.rng.IntN(6)
		if b.rng.IntN(2) == 0 && len(unchanged) > 1 {
			wrong = b.rng.IntN(len(unchanged))
		}
		names := make([]string, len(unchanged))
		for i, t := range unchanged {
			names[i] = t.name
		}
		pq := one(b.rng, []string{
			fmt.Sprintf("not true, there are %d unchanged tasks, not %d", wrong, len(unchanged)),
			fmt.Sprintf("no, %d of them were unchanged", wrong),
			fmt.Sprintf("that's wrong, I count %d unchanged", wrong),
		})
		pa := fmt.Sprintf("The record of run %s shows %d unchanged on %s, not %d: %s.", r.id, len(unchanged), srv.Name, wrong, list(names))
		if len(ran) > 0 {
			rn := make([]string, len(ran))
			for i, t := range ran {
				rn[i] = t.name
			}
			pa += fmt.Sprintf(" The other %d ran (done): %s.", len(ran), list(rn))
		}
		pa += " If you're looking at another run, tell me which one and I'll check it."
		ask("pushback", "summary", turn(sumConv, summary, pq), pa, []string{strconv.Itoa(len(unchanged))})
	}

	// why the ones that ran, ran: a check that found work, or no check at all
	if len(ran) > 0 && len(ran) <= 15 {
		var lines []string
		for _, t := range ran {
			c := sc.cfg.Tasks[t.name]
			if c.Check != "" || c.CheckCopies {
				lines = append(lines, t.name+": its check found something to do, so it ran")
			} else {
				lines = append(lines, t.name+": it has no check, so it runs every time")
			}
		}
		rq := one(b.rng, []string{"Which tasks changed, and why?", "which tasks actually ran and why?", "Why did those tasks run?"})
		ask("ran", "summary", user(rq), fmt.Sprintf("%d tasks ran on %s, and all of them passed:\n\n%s", len(ran), srv.Name, bullets(lines)), []string{ran[0].name})
	}

	// what a task printed, from the record's output
	var printed, silent []simTask
	for _, t := range ran {
		if t.stdout != "" {
			printed = append(printed, t)
		} else {
			silent = append(silent, t)
		}
	}
	if len(printed) > 0 {
		t := one(b.rng, printed)
		o := b.outputFor(t.cmd)
		sum := fill(o.Summary, map[string]string{"server": t.server, "task": t.name})
		q := one(b.rng, []string{"What did %s print?", "summarise the output of %s", "tell me more about the output of %s, what am I looking at?", "what does the stdout of %s say?"})
		ask("output/"+t.name, "output", user(fmt.Sprintf(q, t.name)), fmt.Sprintf("%s on %s ran `%s`. %s", t.name, t.server, strings.TrimSpace(t.cmd), sum), o.Want)
		// asked without the task's name, after the summary: the task is the one that printed
		if len(printed) == 1 {
			nq := one(b.rng, []string{"what can you tell me about the output of the task that was run", "what can you tell me about the output of the tasks command",
				"no I am talking about the tasks stdout, what am I looking at, explain it to me", "explain the output to me", "what am I looking at in the stdout?"})
			ask("output-unnamed/"+t.name, "output", turn(sumConv, summary, nq), fmt.Sprintf("%s on %s ran `%s`. %s", t.name, t.server, strings.TrimSpace(t.cmd), sum), o.Want)
		}
		sq := one(b.rng, []string{"show me the output of %s", "Show me what %s printed", "paste the stdout of %s"})
		ask("show/"+t.name, "output", user(fmt.Sprintf(sq, t.name)),
			fmt.Sprintf("%s printed this on %s (the end of its output, as the run kept it):\n\n```\n%s\n```", t.name, t.server, strings.TrimSpace(t.stdout)), o.Want[:1])
	}
	if len(silent) > 0 {
		t := one(b.rng, silent)
		q := one(b.rng, []string{"show me the output of %s", "what did %s print?"})
		ask("silent/"+t.name, "output", user(fmt.Sprintf(q, t.name)),
			fmt.Sprintf("%s ran on %s and passed, but it printed nothing: the run kept no output from it.", t.name, t.server), []string{"nothing"})
	}
}
