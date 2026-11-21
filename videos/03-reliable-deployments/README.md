# Video 3: Make it reliable: checks, secrets and templates

A 7-minute tutorial: `check` (skip work that's done), `--check` and `--force`, `verify`, `check_copies`,
timeouts, retries and rerun safety, secrets from `${VAR}` and `.env`, environment variables and which
one wins, templates (`{{ .Name }}`, server vars, task vars), template files with `render = true`, then
everything together on a web fleet. Same synthetic voice, pace and look as [video 1](../01-what-is-gorun/)
and [video 2](../02-gorunfile-and-tui/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | captures the output with the release in `GORUN_SRC` |
| `steps/` | small Gorunfiles that really run on this machine: checks, a failing verify, a secret from `.env`, templates (the output in the video is what gorun printed) |
| `demo/` | the web fleet Gorunfile shown on screen: checks, verify, `check_copies`, a rendered page, a secret, per-server env vars (SSH; nothing is contacted) |
| `record/` | the same tasks and checks on local "servers" that sleep and touch marker files, for the fleet screens |

The fleet screens (dry run, first run, rerun, a changed page) are a stand-in run, labelled "Demo run,
sped up" in the video. `record.sh` fails if the secret appears in the captured output.

## Rebuild

Needs ffmpeg, chromium, tmux, Go, unshare, a CUDA GPU and the `chatterbox-tts` Python package (3.11).

```bash
GORUN_SRC=/path/to/latest/gorun ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
