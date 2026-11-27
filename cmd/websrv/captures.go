package websrv

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/envvars"
	"github.com/jenmud/gorun/internal/store"
)

// captureSetting is the settings row an admin turns capture on with.
const captureSetting = "assistant_capture"

// capturing reports whether assistant answers are kept for training: an admin turned it on, or
// GORUN_ASSISTANT_CAPTURE is set. It is off without a history database.
func (a *app) capturing(ctx context.Context) (on, byEnv bool) {
	if a.store == nil {
		return false, false
	}
	if envvars.Bool("GORUN_ASSISTANT_CAPTURE") {
		return true, true
	}
	v, _, err := a.store.Setting(ctx, captureSetting)
	if err != nil {
		slog.ErrorContext(ctx, "read the capture setting", "error", err)
	}
	return v == "on", false
}

// capture keeps an assistant answer when capturing is on, and returns its id, or "".
func (a *app) capture(r *http.Request, c store.Capture) string {
	ctx := r.Context()
	if on, _ := a.capturing(ctx); !on {
		return ""
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	c.ID, c.CreatedAt, c.User, c.Model = hex.EncodeToString(b), time.Now(), a.auth.user(r).Name, a.modelName()
	// the store's context outlives a closed page, so an answer that finished is kept
	if err := a.store.SaveCapture(context.WithoutCancel(ctx), c); err != nil {
		slog.ErrorContext(ctx, "keep an assistant answer", "error", err)
		return ""
	}
	return c.ID
}

// modelName is the model answering: the built-in one's file, or the server's model name.
func (a *app) modelName() string {
	if e := a.engine(); e != nil {
		return e.Stats().Model
	}
	if a.gen == nil {
		return ""
	}
	return cmp.Or(a.gen.assistant.Model, gorun.DefaultAssistantModel)
}

// messagesJSON is a prompt for a capture.
func messagesJSON(messages []gorun.ChatMessage) string {
	data, _ := json.Marshal(messages)
	return string(data)
}

// feedbackHandler records the asker's rating of a kept answer. Anyone who may ask may rate.
func (a *app) feedbackHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Capture string `json:"capture"`
		Value   int    `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Capture == "" || req.Value < -1 || req.Value > 1 {
		writeJSON(w, http.StatusBadRequest, chatReply{Error: "send the capture and a value of -1, 0 or 1"})
		return
	}
	if a.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, chatReply{Error: "the history database isn't open"})
		return
	}
	if err := a.store.SetCaptureFeedback(r.Context(), req.Capture, req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, chatReply{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, chatReply{Reply: "Thanks."})
}

// capturesShown is how many kept answers the page lists; the export has them all.
const capturesShown = 100

// capturesPage lists the kept answers, for an admin to turn capture on or off, review, delete
// and export them.
func (a *app) capturesPage(w http.ResponseWriter, r *http.Request) {
	msg, bad := flashFrom(r)
	d := ui.CapturesPageData{Flash: msg, FlashBad: bad, NoStore: a.store == nil}
	if a.store != nil {
		ctx := r.Context()
		d.On, d.ByEnv = a.capturing(ctx)
		d.Total, _ = a.store.CountCaptures(ctx)
		caps, err := a.store.Captures(ctx, capturesShown)
		if err != nil {
			d.Flash, d.FlashBad = "Couldn't read them: "+err.Error(), true
		}
		for _, c := range caps {
			d.Rows = append(d.Rows, a.captureRow(ctx, c))
		}
	}
	a.page(w, r, "Training data", "assistant", ui.CapturesPage(d))
}

func (a *app) captureRow(ctx context.Context, c store.Capture) ui.CaptureRow {
	row := ui.CaptureRow{
		ID: c.ID, Kind: c.Kind, When: c.CreatedAt.Local().Format("2006-01-02 15:04"), User: c.User, Model: c.Model,
		Question: lastQuestion(c.Messages), Reply: c.Reply, TaskNames: c.TaskNames, TaskError: c.TaskError,
		Repaired: c.Repaired, Saved: !c.SavedAt.IsZero(), Feedback: c.Feedback,
	}
	if row.Saved {
		o, err := a.store.TaskOutcome(ctx, savedTaskNames(c.TaskNames), c.SavedAt)
		if err == nil {
			row.Passed, row.Failed = o.Passed, o.Failed
		}
	}
	return row
}

// lastQuestion is the last thing the user asked in a capture's prompt.
func lastQuestion(messages string) string {
	var ms []gorun.ChatMessage
	_ = json.Unmarshal([]byte(messages), &ms)
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Role == "user" {
			return ms[i].Content
		}
	}
	return ""
}

// savedTaskNames is the task names in a reply's names, without its schedules.
func savedTaskNames(names string) []string {
	var out []string
	for _, n := range strings.Split(names, ", ") {
		if n != "" && !strings.HasPrefix(n, "schedule ") {
			out = append(out, n)
		}
	}
	return out
}

// captureToggleHandler turns capture on or off.
func (a *app) captureToggleHandler(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/assistant/captures", "The history database isn't open.", true)
		return
	}
	on := r.PostFormValue("on") == "on"
	value := map[bool]string{true: "on", false: "off"}[on]
	if err := a.store.SetSetting(r.Context(), captureSetting, value); err != nil {
		redirectWith(w, r, "/assistant/captures", "Couldn't change it: "+err.Error(), true)
		return
	}
	slog.InfoContext(r.Context(), "assistant capture", "on", on, "by", signedIn(r))
	if on {
		redirectWith(w, r, "/assistant/captures", "Capture is on: assistant answers are kept from now on.", false)
		return
	}
	redirectWith(w, r, "/assistant/captures", "Capture is off. What was kept stays until you delete it.", false)
}

// captureDeleteHandler deletes one kept answer, or with no id, all of them.
func (a *app) captureDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/assistant/captures", "The history database isn't open.", true)
		return
	}
	id := r.PostFormValue("id")
	if id == "" && r.PostFormValue("all") != "yes" {
		redirectWith(w, r, "/assistant/captures", "Nothing to delete.", true)
		return
	}
	if err := a.store.DeleteCapture(r.Context(), id); err != nil {
		redirectWith(w, r, "/assistant/captures", "Couldn't delete: "+err.Error(), true)
		return
	}
	if id == "" {
		redirectWith(w, r, "/assistant/captures", "Deleted every kept answer.", false)
		return
	}
	redirectWith(w, r, "/assistant/captures", "Deleted it.", false)
}

// trainingExample is one exported answer, in the shape training/data/*.jsonl has.
type trainingExample struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Messages []gorun.ChatMessage `json:"messages"`
	Meta     trainingMeta        `json:"meta"`
}

// trainingMeta says what became of an answer, to pick the good ones for training.
type trainingMeta struct {
	Created   string `json:"created"`
	Model     string `json:"model"`
	Loaded    bool   `json:"loaded"`               // its tasks loaded
	TaskError string `json:"task_error,omitempty"` // why they didn't
	Repaired  bool   `json:"repaired"`
	Saved     bool   `json:"saved"`
	Feedback  int    `json:"feedback"`
	Passed    int    `json:"runs_passed"` // how its saved tasks did since
	Failed    int    `json:"runs_failed"`
}

// captureExportHandler downloads every kept answer as JSONL, each with the prompt, the reply and
// what became of it. Addresses, e-mail addresses and things that look like keys are replaced
// unless ?redact=0; who asked is never exported.
func (a *app) captureExportHandler(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		http.Error(w, "the history database isn't open", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	caps, err := a.store.Captures(ctx, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	red := newRedactor(r.URL.Query().Get("redact") != "0")
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="gorun-assistant-%s.jsonl"`, time.Now().Format("2006-01-02")))
	enc := json.NewEncoder(w)
	for _, c := range caps {
		var ms []gorun.ChatMessage
		_ = json.Unmarshal([]byte(c.Messages), &ms)
		ms = append(ms, gorun.ChatMessage{Role: "assistant", Content: c.Reply})
		for i := range ms {
			ms[i].Content = red.text(ms[i].Content)
		}
		row := a.captureRow(ctx, c)
		_ = enc.Encode(trainingExample{
			ID: "capture/" + c.ID, Kind: "captured-" + c.Kind, Messages: ms,
			Meta: trainingMeta{
				Created: c.CreatedAt.UTC().Format(time.RFC3339), Model: c.Model,
				Loaded: c.Tasks != "", TaskError: red.text(c.TaskError), Repaired: c.Repaired,
				Saved: row.Saved, Feedback: c.Feedback, Passed: row.Passed, Failed: row.Failed,
			},
		})
	}
}

var (
	ipv4Pattern  = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b`)
	emailPattern = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	// keys and tokens of common services, and private key blocks
	secretPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----|\bAKIA[0-9A-Z]{16}\b|\bgh[pousr]_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{20,}\b|\bxox[abprs]-[A-Za-z0-9-]{10,}\b|\bsk-[A-Za-z0-9_-]{20,}\b|(?i:bearer)\s+[A-Za-z0-9._~+/=-]{20,}`)
)

// redactor replaces what shouldn't leave the machine in exported text. Each address is given
// the same stand-in everywhere, so an example still reads consistently.
type redactor struct {
	on  bool
	ips map[string]string
}

func newRedactor(on bool) *redactor { return &redactor{on: on, ips: map[string]string{}} }

func (r *redactor) text(s string) string {
	if !r.on {
		return s
	}
	s = secretPattern.ReplaceAllString(s, "[redacted]")
	s = emailPattern.ReplaceAllString(s, "user@example.com")
	return ipv4Pattern.ReplaceAllStringFunc(s, func(ip string) string {
		if ip == "127.0.0.1" || ip == "0.0.0.0" {
			return ip
		}
		if _, ok := r.ips[ip]; !ok {
			// TEST-NET-2, kept for documentation
			r.ips[ip] = fmt.Sprintf("198.51.100.%d", len(r.ips)%254+1)
		}
		return r.ips[ip]
	})
}
