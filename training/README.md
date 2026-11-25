# Training the assistant's model

This folder fine-tunes the web UI assistant's model, Qwen3 4B Instruct 2507, on gorun: its
Gorunfile format, its concepts, and diagnosing runs. The result is a GGUF file that
`GORUN_LLM_MODEL` points at:

```bash
GORUN_LLM_MODEL=$PWD/training/out/gorun-qwen3-4b-q4_k_m.gguf gorun -serve Gorunfile
```

It trains a QLoRA adapter (a small set of extra weights on a 4-bit copy of the model), so it
fits an 8 GB GPU. On an RTX 2060 SUPER one epoch takes about six hours.

## The data

`seeds/` holds what the model learns from. All of it is written by hand from the docs:

| File | Holds |
|---|---|
| `generate.toml` | requests for tasks, each with tasks that load; `<<key>>` is filled in from each `params` table |
| `qa.toml` | questions about gorun, a few ways each, with answers from the docs |
| `failures.toml` | ways tasks fail: the error output, its cause and the fix |
| `scenarios/*/Gorunfile` | Gorunfiles the examples happen in, besides the repository's recipes and examples |

`cmd/websrv/dataset_test.go` turns the seeds into conversations, behind the `dataset` build tag.
Every example goes through the real prompts: task requests through `GenerateTasks`, and
everything else through the Assistant tab's chat handler, with the Gorunfile loaded and runs
saved in a history database. So the model learns the exact context it gets in `gorun -serve`.

- It simulates runs (tasks fail, the ones that depend on them are skipped), saves them, and
  writes the diagnosis from the failure catalogue.
- Answers about a Gorunfile (what a server runs, what depends on what) are worked out from the
  loaded config.
- Tasks in replies are checked with `gorun.ExtractTasks`, so every task learned loads.
- About one example in eight is held out for the evaluation.

## Steps

```bash
cd training
uv sync                                     # Python 3.12, PyTorch with CUDA, transformers, peft, trl
HF_HUB_DISABLE_XET=1 HF_HOME=$PWD/.hf .venv/bin/hf download Qwen/Qwen3-4B-Instruct-2507
```

Build llama.cpp's quantizer, for the export (the tag gorun's built-in engine uses):

```bash
mkdir -p .llama.cpp && cd .llama.cpp
curl -fsSL https://github.com/ggml-org/llama.cpp/archive/refs/tags/b12359.tar.gz | tar -xz
cd llama.cpp-b12359 && cmake -B build -DLLAMA_CURL=OFF -DBUILD_SHARED_LIBS=OFF && cmake --build build --target llama-quantize -j
```

Then build the data, train, export and compare:

```bash
go test -tags dataset -run TestBuildDataset ./cmd/websrv     # from the repository root
HF_HOME=$PWD/.hf HF_HUB_OFFLINE=1 .venv/bin/python train.py  # writes out/adapter
HF_HOME=$PWD/.hf HF_HUB_OFFLINE=1 .venv/bin/python export.py # writes out/gorun-qwen3-4b-q4_k_m.gguf
GORUN_EVAL_MODEL=base GORUN_EVAL_N=120 go test -tags dataset -run TestEvalModel -v -timeout 3h ./cmd/websrv
GORUN_EVAL_MODEL=$PWD/training/out/gorun-qwen3-4b-q4_k_m.gguf GORUN_EVAL_N=120 go test -tags dataset -run TestEvalModel -v -timeout 3h ./cmd/websrv
```

The evaluation asks the model each held-out example through gorun's own engine, the way
`gorun -serve` does, and scores the reply. Where the example wants tasks, they must load
(`ExtractTasks`). Every word listed for the example must also appear, such as the failed task
and a word of the error. It prints a score per kind and writes the replies to
`out/eval-<model>.jsonl`.

`.venv/`, `.hf/`, `.llama.cpp/`, `data/` and `out/` are ignored by git.
