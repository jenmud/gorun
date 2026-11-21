# Video 7: OpenTelemetry and NATS

A 5-minute tutorial: sending gorun's traces, metrics and logs to Grafana with OpenTelemetry, and how gorun uses NATS: the events on `gorun.events`, the embedded server that
`gorun -serve` starts, and joining a server that is already running. Same synthetic voice, pace and look
as videos [1](../01-what-is-gorun/), [2](../02-gorunfile-and-tui/), [3](../03-reliable-deployments/),
[4](../04-scaling-up/), [5](../05-web-dashboard/) and [6](../06-go-library/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | runs the instrumented gorun against a local Grafana (`grafana/otel-lgtm`) and NATS, as a made-up user and host, and captures everything |
| `grafana.mjs`, `runweb.mjs`, `natsweb.mjs` | Playwright scripts run by `record.sh`: Grafana's traces, metrics and logs; runs started from the web UI; gorun's NATS header |
| `natssub/` | a small Go program that prints every event on `gorun.events` |
| `record/` | the stand-in web fleet (local servers that only sleep; web-3's smoke test fails) |

The servers are stand-ins, and the video labels them. Grafana, the traces, metrics, logs and NATS events
are all real.

## Rebuild

Needs ffmpeg, chromium, Go, Docker, unshare, node with `playwright-core`, a CUDA GPU and the
`chatterbox-tts` Python package (3.11). `record.sh` pulls `grafana/otel-lgtm` and `nats:2.10-alpine`,
runs them on localhost only and removes them afterwards. The OpenTelemetry part needs the instrumented
build (`make build-otel`, or `GORUN_OTEL_BIN`).

```bash
PW=/path/with/playwright-core ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
