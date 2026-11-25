package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jenmud/gorun"
)

// Engine runs a GGUF model in-process. It loads llama.cpp and the model on its first Chat, and
// holds them until Close. llama.cpp's state is global, so a program has one Engine at a time.
type Engine struct {
	Dir      string    // where the libraries and models are kept
	Model    string    // a GGUF path or https URL; "" is DefaultModel
	Progress io.Writer // downloads and loading report here when set
	Verbose  bool      // let llama.cpp log to stderr

	mu      sync.Mutex
	started bool // llama.cpp is loaded and started
	loaded  bool // the model is loaded
	model   llama.Model
	lctx    llama.Context
	vocab   llama.Vocab
	tmpl    string
	sampler llama.Sampler
}

var _ gorun.Engine = (*Engine)(nil)

const (
	contextSize = 8192 // enough for the prompt, a few retries and their replies
	maxReply    = 1500 // tokens; a few tasks rarely need more
	batchSize   = 2048 // the most tokens llama.cpp decodes at once
	promptChunk = 512  // prompt tokens decoded between checks for a cancelled request
)

func (e *Engine) load(ctx context.Context) error {
	if e.loaded {
		return nil
	}
	libs, err := libraries(ctx, e.Dir, e.Progress)
	if err != nil {
		return err
	}
	file, err := model(ctx, e.Dir, e.Model, e.Progress)
	if err != nil {
		return err
	}
	// llama.cpp is loaded and started once a process, even when the model then fails to load
	if !e.started {
		if err := llama.Load(libs); err != nil {
			return fmt.Errorf("load llama.cpp from %s: %w", libs, err)
		}
		if !e.Verbose {
			llama.LogSet(llama.LogSilent())
		}
		if err := llama.Init(); err != nil {
			return fmt.Errorf("start llama.cpp: %w", err)
		}
		e.started = true
	}

	trace := gorun.ChatTraceFrom(ctx)
	trace.Statusf("Loading the model; it stays loaded for the next question")
	if err := e.open(file, true); err != nil {
		// another program, or another gorun, may hold the GPU's memory; the CPU is slower but has room
		slog.WarnContext(ctx, "the model doesn't fit on the GPU; using the CPU", "error", err)
		trace.Statusf("Not enough GPU memory free, perhaps another program is using it. Loading the model on the CPU instead, which is slower")
		if cerr := e.open(file, false); cerr != nil {
			return fmt.Errorf("%w; on the CPU too: %v", err, cerr)
		}
	}
	e.vocab = llama.ModelGetVocab(e.model)
	e.tmpl = llama.ModelChatTemplate(e.model, "")
	if e.tmpl == "" {
		e.tmpl = "chatml"
	}
	sp := llama.DefaultSamplerParams()
	sp.Temp = 0.2
	e.sampler = llama.NewSampler(e.model, []llama.SamplerType{llama.SamplerTypeTopK, llama.SamplerTypeTopP, llama.SamplerTypeTemperature}, sp)
	e.loaded = true
	return nil
}

// open loads the model from file and makes its context, on the GPU when gpu is set and one is
// found, or else on the CPU. When either step fails, what it loaded is freed, so a retry starts
// with the memory it needs.
func (e *Engine) open(file string, gpu bool) error {
	params := llama.ModelDefaultParams()
	params.NGpuLayers = 0
	if gpu {
		params.NGpuLayers = 999 // as many as fit; with no GPU this is a no-op
	}
	m, err := llama.ModelLoadFromFile(file, params)
	if err != nil || m == 0 {
		return fmt.Errorf("load model %s: %w", filepath.Base(file), errors.Join(err, errors.New("llama.cpp couldn't load it: there isn't enough memory free, or it isn't a GGUF model")))
	}
	cp := llama.ContextDefaultParams()
	cp.NCtx = contextSize
	cp.NBatch = batchSize
	if !gpu { // keep the cache and the work off the GPU too
		cp.Offload_kqv, cp.OpOffload = 0, 0
	}
	lctx, err := llama.InitFromModel(m, cp)
	if err != nil {
		_ = llama.ModelFree(m)
		return fmt.Errorf("start the model: %w; there isn't enough memory free for it", err)
	}
	e.model, e.lctx = m, lctx
	return nil
}

// Chat formats messages with the model's chat template and returns its reply.
func (e *Engine) Chat(ctx context.Context, messages []gorun.ChatMessage) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.load(ctx); err != nil {
		return "", err
	}

	chat := make([]llama.ChatMessage, len(messages))
	for i, m := range messages {
		chat[i] = llama.NewChatMessage(m.Role, m.Content)
	}
	prompt, err := e.format(chat)
	if err != nil {
		return "", err
	}
	// each call starts from an empty context: the whole conversation is in the prompt
	mem, err := llama.GetMemory(e.lctx)
	if err != nil {
		return "", err
	}
	if err := llama.MemoryClear(mem, true); err != nil {
		return "", err
	}

	tokens := llama.Tokenize(e.vocab, prompt, true, true)
	if len(tokens)+maxReply > contextSize {
		return "", fmt.Errorf("the prompt is %d tokens, too long for the model's %d", len(tokens), contextSize)
	}
	trace := gorun.ChatTraceFrom(ctx)
	trace.Statusf("Reading the question, with your Gorunfile and runs (%d tokens)", len(tokens))
	// the prompt goes in batch-sized pieces: llama.cpp aborts the process on a bigger one
	for start := 0; start < len(tokens); start += promptChunk {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := llama.Decode(e.lctx, llama.BatchGetOne(tokens[start:min(start+promptChunk, len(tokens))])); err != nil {
			return "", fmt.Errorf("decode: %w", err)
		}
	}
	trace.Statusf("Writing the reply")
	var reply []byte
	sent := 0 // bytes of reply already given to the trace
	buf := make([]byte, 256)
	for range maxReply {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		token := llama.SamplerSample(e.sampler, e.lctx, -1)
		if llama.VocabIsEOG(e.vocab, token) {
			break
		}
		n := llama.TokenToPiece(e.vocab, token, buf, 0, false)
		if n < 0 { // the piece needs -n bytes
			buf = make([]byte, -n)
			n = llama.TokenToPiece(e.vocab, token, buf, 0, false)
		}
		reply = append(reply, buf[:max(n, 0)]...)
		if end := wholeRunes(reply); end > sent {
			trace.Piece(string(reply[sent:end]))
			sent = end
		}
		if _, err := llama.Decode(e.lctx, llama.BatchGetOne([]llama.Token{token})); err != nil {
			return "", fmt.Errorf("decode: %w", err)
		}
	}
	return string(reply), nil
}

// format applies the chat template, growing the buffer when the first one is too small.
func (e *Engine) format(chat []llama.ChatMessage) (string, error) {
	buf := make([]byte, 16<<10)
	n := llama.ChatApplyTemplate(e.tmpl, chat, true, buf)
	if int(n) > len(buf) {
		buf = make([]byte, n)
		n = llama.ChatApplyTemplate(e.tmpl, chat, true, buf)
	}
	if n < 0 || int(n) > len(buf) {
		return "", fmt.Errorf("the model's chat template %q isn't one llama.cpp knows", e.tmpl)
	}
	return string(buf[:n]), nil
}

// Close frees the model and llama.cpp.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded {
		return
	}
	llama.SamplerFree(e.sampler)
	_ = llama.Free(e.lctx)
	_ = llama.ModelFree(e.model)
	llama.Close()
	e.loaded = false
}

// wholeRunes is the length of b without a UTF-8 character it ends partway through, which a token
// can do; the rest of the character comes with the next one.
func wholeRunes(b []byte) int {
	for k := 1; k <= min(utf8.UTFMax-1, len(b)); k++ {
		if i := len(b) - k; utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return i
			}
			break
		}
	}
	return len(b)
}
