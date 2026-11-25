# Video 10: The built-in assistant

A 3-minute tutorial: the Assistant tab in `gorun -serve` (what it downloads on first use, and that it is
private), asking why a run failed from a run's History page, summaries and questions about the Gorunfile,
having it write a task and adding it as an unsaved change, the Config page's Generate tasks form, and
using other models with `GORUN_LLM_MODEL` and `GORUN_LLM_URL`. Same synthetic voice, pace and look as
videos [1](../01-what-is-gorun/) to [9](../09-copies/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | starts `gorun -serve` on the `record/` fleet under a made-up host and user name |
| `capture.mjs` | drives the web UI with Playwright and screenshots each step (run by `record.sh`) |
| `record/` | the web fleet on local "servers" that only sleep; nginx fails on web-3, so its site and smoke test are skipped |

The screenshots are the real web UI, and every answer in them is the built-in model's own (it is the
stock Qwen3 4B Instruct, from gorun's cache folder; `record.sh` waits for each reply). The servers are
stand-ins, which the video labels "Demo run: stand-in servers, real model answers". The first-use
download text on screen shows what a reader would see.

The answers differ a little on each recording. After re-recording, read `build/cap/replies.json` and check
that `narration.py` still describes them: the port 80 diagnosis, the redis task with a check and a verify.

## Rebuild

Needs ffmpeg, chromium, Go, unshare, node with `playwright-core`, a GPU (for the model and the voice) and
the `chatterbox-tts` Python package (3.11).

```bash
PW=/path/with/playwright-core GORUN_SRC=/path/to/latest/gorun ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
