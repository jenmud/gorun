#!/usr/bin/env python3
"""Builds video 7, "OpenTelemetry and NATS": real Grafana screenshots, gorun's NATS events, slides and a
Chatterbox voiceover -> MP4, captions and chapters.

    PW=/path/with/playwright-core ./record.sh     # captures everything (needs Go, Docker, unshare, chromium, node)
    ./build.py                                    # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import base64
import html
import os
import re
import sys
from pathlib import Path

# same pace as the other videos (~196 words/min)
os.environ.setdefault("TEMPO", "1.22")

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import code, page, shell, slide, text_file, window  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
BADGE = "Demo run: stand-in servers"


def b64(name):
    return base64.b64encode((CAP / f"{name}.png").read_bytes()).decode()


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">OTEL_EXPORTER_OTLP_ENDPOINT · nats://</div>'
        "<h1>OpenTelemetry <span class=acc>and NATS</span></h1>"
        '<div class="sub">Watch gorun from the outside.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 7 of the series</div></div>'
    )


def shot(name, caption="", badge=True, height=930):
    tag = f'<div class="badge">{BADGE}</div>' if badge else ""
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center;padding:30px">'
        f'<img style="max-height:{height}px;max-width:1860px;border-radius:14px;border:1px solid var(--line)" src="data:image/png;base64,{b64(name)}">'
        + (f'<div class="sub" style="margin-top:18px;font-size:28px">{caption}</div>' if caption else "")
        + f"</div>{tag}"
    )


def pair(a, b, ca, cb):
    cell = lambda n, c: (f'<div style="margin:22px 0"><img style="width:1700px;border-radius:12px;border:1px solid var(--line)" src="data:image/png;base64,{b64(n)}">'
                         f'<div class="sub" style="margin-top:10px;font-size:28px">{c}</div></div>')
    return page('<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center">'
                + cell(a, ca) + cell(b, cb) + "</div>")


def clip(name, y0, y1, caption):
    """Rows y0..y1 (CSS pixels) of a full-page screenshot taken at 2x, shown at 1.7x."""
    k = 1860 / 1100
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center;padding:30px">'
        f'<div style="width:1860px;height:{(y1 - y0) * k:.0f}px;overflow:hidden;border-radius:14px;border:1px solid var(--line)">'
        f'<img style="width:1860px;margin-top:-{y0 * k:.0f}px" src="data:image/png;base64,{b64(name)}"></div>'
        f'<div class="sub" style="margin-top:18px;font-size:28px">{caption}</div></div><div class="badge">{BADGE}</div>'
    )


def startup(name):
    # the capture used a random port; a reader sees the defaults
    lines = text_file(name)
    lines[0] = "gorun is serving on http://localhost:8080 (listening on :8080, all interfaces)"
    lines = [ln.replace("/home/dev/web/h.db", "/home/dev/.gorun/gorun.db").replace("/home/dev/web/gorun.log", "/home/dev/.gorun/gorun.log") for ln in lines]
    return [("out", ln) for ln in lines if not ln.startswith("Terminated") and not ln.strip().startswith(("mount", "hostname", "cd ", "exec", "printf"))]


def gofile(path, title, start, end):
    text = Path(path).read_text().replace("\t", "    ")
    return code(text, start, end, title, lang="go", window_lines=24, font=28)


NATSSUB = HERE / "natssub" / "main.go"
FILELOG = (HERE / "record" / "filelog.yaml").read_text()
NATSCONF = 'host: "0.0.0.0"\nport: 4222\n'

natssub_lines = text_file("natssub")
natssub_lines = natssub_lines[: next(i for i, ln in enumerate(natssub_lines) if ln.startswith("process_finished")) + 1]  # then the shell reports the subscriber stopping
nl = NATSSUB.read_text().split("\n")
i_main = next(i for i, ln in enumerate(nl) if ln.startswith("func main"))

SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("otelwhat", None, [
        (5, shell([("cmd", "OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 gorun -serve Gorunfile"),
                   ("out", "# traces and metrics go to your collector; GORUN_NAME is the service name")], "bash")),
        (5, slide("Instrumented build", [
            ("", "The install script's default", "the standard build has no OpenTelemetry"),
            ("", "Off until you configure it", "no endpoint, nothing is sent"),
            ("", "Use it with gorun -serve", "a one-shot command can exit before the last spans are sent"),
        ])),
    ]),
    ("traces", None, [
        (4, shot("traces-search", "Tempo: one trace for every run")),
        (6, shot("trace", "One span for the run, one for each server, one for each task")),
        (6, shot("trace-attrs", "The run span: who ran it, where, and the totals")),
    ]),
    ("metrics", None, [
        (5, shot("metrics-count", "Prometheus: task executions by server and status")),
        (5, shot("metrics-duration", "Prometheus: how long each task takes (95th percentile)")),
        (4, slide("Metrics gorun sends", [
            ("", "gorun.task.executions", "a counter, by server, task and status"),
            ("", "gorun.task.duration", "a histogram of task times, in seconds"),
            ("", "gorun.task.skipped", "tasks skipped by a failed dependency"),
        ])),
    ]),
    ("logs", None, [
        (3, shell([("cmd", "tail -n 1 ~/.gorun/gorun.log")] + [
            ("out", "time=… level=ERROR msg=\"task failed\" name=smoke … gorun_uuid=web-428372-…")], "bash")),
        (7, code(FILELOG, 0, len(FILELOG.rstrip("\n").split("\n")), "filelog.yaml", window_lines=36, font=22)),
        (5, shot("logs-errors", "Loki: gorun's log file, filtered to errors")),
    ]),
    ("natswhat", None, [
        (4, slide("gorun publishes every event on NATS", [
            ("", "gorun.events", "the subject, as JSON"),
            ("", "process_started, plan, task, process_finished", "and server_error, copy_progress"),
            ("", "The live dashboard listens", "and so can you"),
        ])),
        (6, gofile(NATSSUB, "natssub/main.go", i_main + 6, i_main + 16)),
        (6, shell([("cmd", "go run ./natssub")] + [("out", ln) for ln in natssub_lines])),
    ]),
    ("natsembedded", None, [
        (6, shell([("cmd", "gorun -serve Gorunfile")] + startup("serve-embedded"))),
        (4, shot("embedded-header", "gorun -serve: the header says how it reaches NATS", badge=False, height=300)),
        (5, shot("embedded-live", "a run started from a terminal shows up on the Live page (stand-in servers)", badge=False)),
        (6, shell([("cmd", "cat nats.conf")] + [("out", ln) for ln in NATSCONF.strip().split("\n")]
                  + [("cmd", "GORUN_NATS_SERVER_CONFIG=nats.conf gorun -serve Gorunfile")])),
    ]),
    ("natsexternal", None, [
        (5, shell([("cmd", "docker run -d -p 4222:4222 nats:2.10-alpine"),
                   ("cmd", "GORUN_NATS_CLIENT_URL=nats://127.0.0.1:4222 gorun -serve Gorunfile")] +
                  [("out", ln) for _, ln in startup("serve-external")[:3]])),
        (5, pair("embedded-header", "external-header", "embedded: gorun started the server", "external: gorun joined one that was already running")),
        (6, page('<div class="wrap"><h2>NATS settings</h2><table style="font-size:32px">' + "".join(
            f'<tr><td class="mono acc" style="white-space:nowrap">{html.escape(a)}</td><td>{html.escape(b)}</td></tr>' for a, b in [
                ("GORUN_NATS_CLIENT_URL", "use this server, and start none"),
                ("GORUN_NATS_SERVER_CONFIG", "config for the embedded server"),
                ("GORUN_NATS_CLIENT_NAME", "this client's name"),
                ("GORUN_NATS_CLIENT_ALLOW_RECONNECT", "reconnect automatically"),
                ("GORUN_EVENT_TOPIC", "the subject, gorun.events by default"),
                ("GORUN_SKIP_NATS=1", "never start or use NATS"),
            ]) + '</table></div><div class="brand mono">gorun</div>', "td{padding:14px 26px}")),
    ]),
    ("outro", None, [(1, slide("Recap", [
        ("", "OpenTelemetry", "traces and metrics in Grafana"),
        ("", "gorun.log", "shipped with a collector"),
        ("", "NATS", "embedded, or one you already run"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "otelwhat": "OpenTelemetry",
    "traces": "Traces",
    "metrics": "Metrics",
    "logs": "Logs",
    "natswhat": "What NATS does",
    "natsembedded": "The embedded server",
    "natsexternal": "Joining a NATS server",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "07-otel-and-nats")
