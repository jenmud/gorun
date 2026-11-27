package llm

import (
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/hybridgroup/yzma/pkg/llama"
)

// Stats is what the engine knows about itself, for a page to show. It is a copy, so reading it
// never waits for an answer the model is writing.
type Stats struct {
	Model      string // the model's file name
	Downloaded bool   // the model file is on disk
	Loaded     bool   // the model is in memory, ready to answer

	// set once loaded
	Desc       string // llama.cpp's description, such as "qwen3 4B Q4_K - Medium"
	Size       uint64 // bytes of weights
	Params     uint64
	Layers     int32
	GPULayers  int32 // how many layers are on the GPU; 0 runs it all on the CPU
	CPUReason  string
	Context    int // tokens the model can hold: the prompt and the reply
	MaxReply   int // tokens
	LoadedIn   time.Duration
	GPU        string // the GPU llama.cpp found, if any
	GPUFree    uint64 // its memory, as the driver reports it
	GPUTotal   uint64
	Answers    int
	LastAnswer *AnswerStats
}

// AnswerStats is how one answer went.
type AnswerStats struct {
	PromptTokens, ReplyTokens int
	Reading, Writing          time.Duration
}

// Stats returns what the engine knows now. GPU memory is read again when no answer is being
// written.
func (e *Engine) Stats() Stats {
	if e.mu.TryLock() {
		if e.loaded {
			e.readGPU()
		}
		e.mu.Unlock()
	}
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	s := e.stats
	if s.Model == "" {
		s.Model = e.modelFile()
	}
	if !s.Loaded {
		_, err := os.Stat(filepath.Join(e.Dir, "models", s.Model))
		s.Downloaded = err == nil || fileExists(e.Model)
	}
	if s.LastAnswer != nil {
		last := *s.LastAnswer
		s.LastAnswer = &last
	}
	return s
}

// modelFile is the name of the model file the engine uses.
func (e *Engine) modelFile() string {
	if e.Model == "" {
		return path.Base(DefaultModel.URL)
	}
	return path.Base(filepath.ToSlash(e.Model))
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// noteLoaded records the model just loaded. e.mu is held.
func (e *Engine) noteLoaded(gpuLayers int32, cpuReason string, took time.Duration) {
	e.statsMu.Lock()
	e.stats.Model = e.modelFile()
	e.stats.Downloaded, e.stats.Loaded = true, true
	e.stats.Desc = llama.ModelDesc(e.model)
	e.stats.Size = llama.ModelSize(e.model)
	e.stats.Params = llama.ModelNParams(e.model)
	e.stats.Layers = llama.ModelNLayer(e.model)
	e.stats.GPULayers = min(gpuLayers, e.stats.Layers)
	e.stats.CPUReason = cpuReason
	e.stats.Context, e.stats.MaxReply = contextSize, maxReply
	e.stats.LoadedIn = took
	e.statsMu.Unlock()
	e.readGPU()
}

// noteAnswer records how an answer went. e.mu is held.
func (e *Engine) noteAnswer(a AnswerStats) {
	e.statsMu.Lock()
	e.stats.Answers++
	e.stats.LastAnswer = &a
	e.statsMu.Unlock()
	e.readGPU()
}

// readGPU reads the first GPU's name and memory. e.mu is held, as llama.cpp is then idle.
func (e *Engine) readGPU() {
	for i := range llama.GGMLBackendDeviceCount() {
		dev := llama.GGMLBackendDeviceGet(i)
		if llama.GGMLBackendDevType(dev) != llama.GGMLBackendDeviceTypeGPU {
			continue
		}
		free, total := llama.GGMLBackendDeviceMemory(dev)
		e.statsMu.Lock()
		e.stats.GPU = llama.GGMLBackendDeviceDescription(dev)
		e.stats.GPUFree, e.stats.GPUTotal = free, total
		e.statsMu.Unlock()
		return
	}
}
