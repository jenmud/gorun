# Video 5: The web dashboard

A 3-minute tutorial: `gorun -serve` (the dashboard, the embedded NATS server, the history database and
the scheduler), why it has no login and how to keep it private, the Run, Live, History, Tasks and
Schedules pages, and runs that start elsewhere appearing on Live. Same synthetic voice, pace and look as
videos [1](../01-what-is-gorun/), [2](../02-gorunfile-and-tui/), [3](../03-reliable-deployments/) and
[4](../04-scaling-up/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | starts `gorun -serve` on the `record/` fleet under a made-up host and user name |
| `capture.mjs` | drives the web UI with Playwright and screenshots each page (run by `record.sh`) |
| `record/` | the web fleet on local "servers" that only sleep (web-3's smoke test fails), plus one schedule |

The screenshots are the real web UI, running the stand-in fleet, which the video labels "Demo run:
stand-in servers". The start-up text on screen shows the defaults a reader would see.

## Rebuild

Needs ffmpeg, chromium, Go, unshare, node with `playwright-core`, a CUDA GPU and the `chatterbox-tts`
Python package (3.11).

```bash
PW=/path/with/playwright-core GORUN_SRC=/path/to/latest/gorun ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
