#!/usr/bin/env python3
"""Builds video 6, "Using gorun as a Go library": real code and output from the programs in demo/, the two
example programs, slides and a Chatterbox voiceover -> MP4, captions and chapters.

    PW=/path/with/playwright-core ./record.sh     # runs the programs (needs Go, tmux, unshare, chromium, node)
    ./build.py                                    # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import base64
import os
import sys
from pathlib import Path

# same pace as the other videos (~196 words/min)
os.environ.setdefault("TEMPO", "1.22")

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import code, page, shell, slide, text_file, tmux  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP


def body(name):
    """A demo program from `func main` on, as one string."""
    text = (HERE / "demo" / name / "main.go").read_text()
    return text[text.index("func main()"):].replace("\t", "    ")


def go(name, start, end):
    text = body(name)
    return code(text, start, end, f"{name}/main.go", lang="go", window_lines=24, font=28)


def out(cmd, name, keep=None):
    lines = [ln for ln in text_file(name) if ln.strip()]
    if keep:
        lines = [ln for ln in lines if keep(ln)]
    return shell([("cmd", cmd)] + [("out", ln) for ln in lines])


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">import "github.com/jenmud/gorun"</div>'
        "<h1>gorun as a <span class=acc>Go library</span></h1>"
        '<div class="sub">Run a Gorunfile from your own code.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 6 of the series</div></div>'
    )


def shot(name, caption):
    b64 = base64.b64encode((CAP / f"{name}.png").read_bytes()).decode()
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center;padding:30px">'
        f'<img style="max-height:930px;max-width:1860px;border-radius:14px;border:1px solid var(--line)" src="data:image/png;base64,{b64}">'
        f'<div class="sub" style="margin-top:18px;font-size:28px">{caption}</div></div>'
    )


def gorunfile(part):
    text = (HERE / "demo" / "Gorunfile").read_text()
    n = len(text.rstrip("\n").split("\n"))
    return code(text, *((0, 25) if part == 1 else (25, n)), "Gorunfile", window_lines=25, font=26)


def not_log(ln):
    return not ln.startswith("20")  # the standard library logger's own lines


SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("why", None, [(1, slide("Why use it as a library?", [
        ("", "A button in your web app", "that restarts a service"),
        ("", "A release tool", "with its own progress bars"),
        ("", "A bot", "that runs a task when someone asks"),
    ]))]),
    ("hello", None, [
        (3, shell([("cmd", "go get github.com/jenmud/gorun")])),
        (3, gorunfile(1)),
        (2, gorunfile(2)),
        (4, go("hello", 2, 7)),
        (4, go("hello", 8, 13)),
        (3, out("go run ./hello", "hello")),
    ]),
    ("events", None, [
        (5, go("events", 7, 20)),
        (3, slide("What OnEvent receives", [
            ("", "TaskEvent", "running, done, failed or skipped, with output and exit code"),
            ("", "ProcessFinishedEvent", "the totals, when the run ends"),
            ("", "Called concurrently", "one goroutine per server, so lock your state"),
        ])),
        (3, go("events", 21, 24)),
        (5, out("go run ./events", "events", not_log)),
    ]),
    ("select", None, [
        (5, go("pick", 12, 24)),
        (3, go("pick", 25, 28)),
        (4, out("go run ./pick", "pick", not_log)),
    ]),
    ("history", None, [
        (4, go("hist", 3, 8)),
        (3, go("hist", 13, 15)),
        (4, go("hist", 16, 23)),
        (3, out("go run ./hist", "hist")),
    ]),
    ("encode", None, [
        (4, go("encode", 6, 12)),
        (4, out("go run ./encode", "encode")),
    ]),
    ("examples", None, [
        (4, tmux("rollout-mid", title="examples/rollout", minw=1500, font=28)),
        (4, tmux("rollout-done", title="examples/rollout", minw=1500, font=28)),
        (4, shot("deck-running", "examples/servicedeck: a web app built on the library")),
        (4, shot("deck-done", "examples/servicedeck: the run finished")),
    ]),
    ("outro", None, [(1, slide("Recap", [
        ("", "LoadFile, Run", "run a Gorunfile from your code"),
        ("", "OnEvent, Recorder", "follow a run, and keep its history"),
        ("", "SelectServers, RunTask", "choose what runs, like the flags"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "why": "Why a library?",
    "hello": "The smallest program",
    "events": "Following a run",
    "select": "Choosing what runs",
    "history": "Recording history",
    "encode": "Writing a Gorunfile",
    "examples": "Two examples",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "06-go-library")
