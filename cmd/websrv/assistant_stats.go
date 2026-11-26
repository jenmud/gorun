package websrv

import (
	"cmp"
	"fmt"
	"net/http"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// assistantStats describes the assistant's model for the Assistant page: where it runs, its
// memory, and how its last answer went.
func (a *app) assistantStats() ui.AssistantStats {
	g := a.gen
	if g == nil {
		return ui.AssistantStats{State: "The assistant is off."}
	}
	if g.engine == nil {
		where := "on this machine"
		if !g.private() {
			where = "not on this machine, so questions are sent there"
		}
		return ui.AssistantStats{State: "Answered by a model server", Rows: []ui.StatRow{
			{Label: "Model", Value: cmp.Or(g.assistant.Model, gorun.DefaultAssistantModel)},
			{Label: "Server", Value: g.assistant.URL, Note: where},
			{Label: "Runs on", Value: "the server's own GPU or CPU", Note: "gorun can't see its memory"},
		}}
	}

	s := g.engine.Stats()
	out := ui.AssistantStats{}
	switch {
	case !s.Downloaded:
		out.State = "Not downloaded yet: the first question downloads it, about 2.5 GB, then loads it"
	case !s.Loaded:
		out.State = "Downloaded; the first question loads it"
	case s.GPULayers > 0:
		out.State, out.Tone = "Loaded on the GPU", "good"
	default:
		out.State, out.Tone = "Loaded on the CPU, which is slower: "+s.CPUReason, "warn"
	}
	out.Rows = append(out.Rows, ui.StatRow{Label: "Model", Value: s.Model, Note: s.Desc})
	if !s.Loaded {
		out.Rows = append(out.Rows, ui.StatRow{Label: "Files", Value: g.engine.Dir})
		return out
	}
	out.Rows = append(out.Rows, ui.StatRow{Label: "Size", Value: fmt.Sprintf("%s, %.1f billion parameters", bytesText(s.Size), float64(s.Params)/1e9)})
	switch {
	case s.GPULayers >= s.Layers:
		out.Rows = append(out.Rows, ui.StatRow{Label: "Runs on", Value: "GPU: " + s.GPU, Note: fmt.Sprintf("all %d layers", s.Layers)})
	case s.GPULayers > 0:
		out.Rows = append(out.Rows, ui.StatRow{Label: "Runs on", Value: "GPU: " + s.GPU, Note: fmt.Sprintf("%d of %d layers; the rest on the CPU", s.GPULayers, s.Layers)})
	default:
		out.Rows = append(out.Rows, ui.StatRow{Label: "Runs on", Value: "CPU", Note: s.CPUReason})
	}
	if s.GPUTotal > 0 {
		out.Rows = append(out.Rows, ui.StatRow{Label: "GPU memory", Value: fmt.Sprintf("%s free of %s", bytesText(s.GPUFree), bytesText(s.GPUTotal)), Note: s.GPU})
	}
	out.Rows = append(out.Rows,
		ui.StatRow{Label: "Context", Value: fmt.Sprintf("%d tokens", s.Context), Note: fmt.Sprintf("the question, your Gorunfile and runs, and a reply of up to %d", s.MaxReply)},
		ui.StatRow{Label: "Loaded in", Value: secondsText(s.LoadedIn)},
	)
	if l := s.LastAnswer; l != nil {
		speed := ""
		if l.Writing > 0 && l.ReplyTokens > 0 {
			speed = fmt.Sprintf("%.0f tokens a second", float64(l.ReplyTokens)/l.Writing.Seconds())
		}
		out.Rows = append(out.Rows, ui.StatRow{
			Label: "Last answer",
			Value: fmt.Sprintf("read %d tokens in %s, wrote %d in %s", l.PromptTokens, secondsText(l.Reading), l.ReplyTokens, secondsText(l.Writing)),
			Note:  speed,
		})
	}
	out.Rows = append(out.Rows, ui.StatRow{Label: "Answers", Value: fmt.Sprint(s.Answers), Note: "since gorun started"})
	return out
}

// assistantStatsHandler draws the stats panel again, for the page to refresh after an answer.
func (a *app) assistantStatsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = ui.AssistantStatsPanel(a.assistantStats()).Render(a.renderCtx(), w)
}

func bytesText(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}

func secondsText(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}
