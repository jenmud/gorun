# Training the assistant's model

This folder fine-tunes the web UI assistant's model, Qwen3 4B Instruct 2507, on gorun: its
Gorunfile format, its concepts, and diagnosing runs. The result is a GGUF file, published at
[jenmud/gorun-qwen3-4b-GGUF](https://huggingface.co/jenmud/gorun-qwen3-4b-GGUF), which is gorun's
built-in model (`DefaultModel` in `internal/llm/download.go`). Try a new build before publishing
it with `GORUN_LLM_MODEL`:

```bash
GORUN_LLM_MODEL=$PWD/training/out/gorun-qwen3-4b-q4_k_m.gguf gorun -serve Gorunfile
```

It trains a QLoRA adapter (a small set of extra weights on a 4-bit copy of the model), so it
fits an 8 GB GPU: examples up to 3,072 tokens take about 5.2 GB. On an RTX 2060 SUPER one epoch
takes about two and a half hours. An RTX 20 has no bf16 or
flash attention, so `train.py` trains in fp16 and keeps SDPA on its memory-efficient kernel; the
math kernel it would otherwise fall back to runs out of memory on the longest examples.

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
- Tasks and schedules in replies are checked with `gorun.ExtractTasks`, so every one learned
  loads. Schedule requests and changes are built from each Gorunfile's tasks and schedules.
- Questions carry the doc sections the chat handler finds for them (`docs/docs.go`), so the model
  learns to answer from them. A docs change reaches answers at once, but training on it again
  keeps the examples in step.
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
GORUN_EVAL_MODEL=https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q4_K_M.gguf GORUN_EVAL_N=120 go test -tags dataset -run TestEvalModel -v -timeout 3h ./cmd/websrv
GORUN_EVAL_MODEL=$PWD/training/out/gorun-qwen3-4b-q4_k_m.gguf GORUN_EVAL_N=120 go test -tags dataset -run TestEvalModel -v -timeout 3h ./cmd/websrv
```

The evaluation asks the model each held-out example through gorun's own engine, the way
`gorun -serve` does, and scores the reply. Where the example wants tasks, they must load
(`ExtractTasks`, or `ExtractTaskEdits` where the example changes a task). Every word listed for the example must also appear, such as the failed task
and a word of the error. It prints a score per kind and writes the replies to
`out/eval-<model>.jsonl`.

To build on the last model rather than start from the base, keep its adapter and continue it at
a lower learning rate; v4 was v3's adapter trained one more epoch at 1e-4. Score the old model on
the new held-out set too, so the two are compared on the same questions:

```bash
HF_HOME=$PWD/.hf HF_HUB_OFFLINE=1 .venv/bin/python train.py --from-adapter out/v3-adapter --out out/adapter-v4 --lr 1e-4
HF_HOME=$PWD/.hf HF_HUB_OFFLINE=1 .venv/bin/python export.py --adapter out/adapter-v4 --name gorun-qwen3-4b-v4
```

`.venv/`, `.hf/`, `.llama.cpp/`, `data/` and `out/` are ignored by git.

## Real questions from gorun -serve

The seeds are written by hand. Real questions, from people using the assistant, show what they
actually ask and where the model goes wrong. Turn on capture on the Assistant page's **Training
data** page (see [the web UI](../docs/web.md#training-data)), use the assistant for a while, then
**Export JSONL**. Each line has `messages`, as `train.jsonl` does, and a `meta` saying what became
of the answer: `loaded` and `task_error`, `repaired`, `saved`, `feedback` (1 for 👍, -1 for 👎)
and `runs_passed` and `runs_failed`, how the tasks it wrote ran once saved.

Read the export first: it holds your Gorunfile's hostnames and task output. Then keep the answers
that were right, and add them after building the data, since the build writes `data/train.jsonl`
afresh:

```bash
jq -c 'select(.meta.feedback >= 0 and (.meta.task_error // "") == "" and .meta.runs_failed == 0)
       | {id, kind, messages}' gorun-assistant-*.jsonl >> data/train.jsonl
```

A 👎 answer, or one whose tasks failed, is worth more as a new seed: write the right answer into
`seeds/` by hand, so every build learns it.

## Publishing

Upload each build under a new file name, such as `gorun-qwen3-4b-v5-q4_k_m.gguf`: gorun keeps a
downloaded model by its file name and never downloads one it has, so a build uploaded over the old
name would never reach machines that have the old one. Then point `DefaultModel` at the new URL
with its SHA-256, and `DEFAULT_MODEL` in the Makefile at the same URL:

```bash
cp out/gorun-qwen3-4b-q4_k_m.gguf out/hf/gorun-qwen3-4b-v5-q4_k_m.gguf
sha256sum out/hf/gorun-qwen3-4b-v5-q4_k_m.gguf
HF_HOME=$PWD/.hf .venv/bin/hf upload jenmud/gorun-qwen3-4b-GGUF out/hf/gorun-qwen3-4b-v5-q4_k_m.gguf
HF_HOME=$PWD/.hf .venv/bin/hf upload jenmud/gorun-qwen3-4b-GGUF out/hf/README.md
```
