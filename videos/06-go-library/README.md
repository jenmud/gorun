# Video 6: Using gorun as a Go library

A 4-minute tutorial: loading and running a Gorunfile from Go, following a run with `OnEvent`, choosing
servers and tasks like the command-line flags, recording history with the `history` package, writing a
Gorunfile with `Config.Encode`, and the two example programs. Same synthetic voice, pace and look as
videos [1](../01-what-is-gorun/), [2](../02-gorunfile-and-tui/), [3](../03-reliable-deployments/),
[4](../04-scaling-up/) and [5](../05-web-dashboard/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | builds and runs every program in `demo/`, plus `examples/rollout` (in tmux) and `examples/servicedeck` (with Playwright) |
| `capture.mjs` | drives Service Deck and screenshots a run (run by `record.sh`) |
| `demo/` | the programs shown on screen and the Gorunfile they run, a small module that uses the gorun beside it |

Everything on screen is really run. The servers in `demo/Gorunfile` are local and only echo and sleep,
and web-2's deploy fails on purpose.

## Rebuild

Needs ffmpeg, chromium, Go, tmux, unshare, node with `playwright-core`, a CUDA GPU and the
`chatterbox-tts` Python package (3.11).

```bash
PW=/path/with/playwright-core ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
