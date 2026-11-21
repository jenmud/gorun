# Video 8: gorun in CI and automation

A 5-minute tutorial: running gorun without the picker (`-r --plain`), exit codes, aiming a run with
filters, previewing with `--check`, secrets from the environment, how gorun records who ran what (the CI
system, the user and your own labels), an example GitHub Actions workflow, and cron or `gorun -serve`
schedules. Same synthetic voice, pace and look as videos [1](../01-what-is-gorun/),
[2](../02-gorunfile-and-tui/), [3](../03-reliable-deployments/), [4](../04-scaling-up/),
[5](../05-web-dashboard/), [6](../06-go-library/) and [7](../07-otel-and-nats/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | runs gorun on `demo/` as a made-up user and host and captures the output and exit codes |
| `ciweb.mjs` | Playwright script run by `record.sh`: screenshots the web UI's History pages |
| `demo/` | the Gorunfile: a small release on stand-in servers (local, they only sleep); prod-2's smoke test fails when `SMOKE_FAIL=1` |
| `workflow.yml` | the example GitHub Actions workflow shown on screen; it is not run |

The runs, exit codes and history are real. The CI run sets the variables GitHub Actions sets, by hand, so
the history shows it as a CI run; the video labels those screenshots "Simulated CI variables".

## Rebuild

Needs ffmpeg, chromium, Go, unshare, node with `playwright-core`, a CUDA GPU and the `chatterbox-tts`
Python package (3.11).

```bash
PW=/path/with/playwright-core ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
