package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	loaded  bool
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
	batchSize   = 2048 // tokens decoded at once
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
	if err := llama.Load(libs); err != nil {
		return fmt.Errorf("load llama.cpp from %s: %w", libs, err)
	}
	if !e.Verbose {
		llama.LogSet(llama.LogSilent())
	}
	if err := llama.Init(); err != nil {
		return fmt.Errorf("start llama.cpp: %w", err)
	}

	gorun.ChatTraceFrom(ctx).Statusf("Loading the model; it stays loaded for the next question")
	params := llama.ModelDefaultParams()
	params.NGpuLayers = 999 // as many as fit; with no GPU this is a no-op
	if e.model, err = llama.ModelLoadFromFile(file, params); err != nil || e.model == 0 {
		return fmt.Errorf("load model %s: %w", filepath.Base(file), errors.Join(err, errors.New("llama.cpp couldn't load it: it isn't a GGUF model, or there isn't enough GPU or system memory free")))
	}
	cp := llama.ContextDefaultParams()
	cp.NCtx = contextSize
	cp.NBatch = batchSize
	if e.lctx, err = llama.InitFromModel(e.model, cp); err != nil {
		return fmt.Errorf("model context: %w", err)
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
	for start := 0; start < len(tokens); start += batchSize {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := llama.Decode(e.lctx, llama.BatchGetOne(tokens[start:min(start+batchSize, len(tokens))])); err != nil {
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
