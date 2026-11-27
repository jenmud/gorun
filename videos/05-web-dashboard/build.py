#!/usr/bin/env python3
"""Builds video 5, "The web dashboard": real screenshots of gorun -serve, slides and a Chatterbox
voiceover -> MP4, captions and chapters.

    PW=/path/with/playwright-core ./record.sh     # captures the web UI (needs Go, unshare, chromium, node)
    ./build.py                                    # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import base64
import os
import re
import sys
from pathlib import Path

# same pace as the other videos (~196 words/min)
os.environ.setdefault("TEMPO", "1.22")

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import page, shell, slide, text_file  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
BADGE = "Demo run: stand-in servers"


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">$ gorun -serve</div>'
        "<h1>The web <span class=acc>dashboard</span></h1>"
        '<div class="sub">Run, watch and look back, from a browser.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 5 of the series</div></div>'
    )


def shot(name, caption="", badge=True):
    b64 = base64.b64encode((CAP / f"{name}.png").read_bytes()).decode()
    tag = f'<div class="badge">{BADGE}</div>' if badge else ""
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center;padding:30px">'
        f'<img style="max-height:930px;max-width:1860px;border-radius:14px;border:1px solid var(--line)" src="data:image/png;base64,{b64}">'
        + (f'<div class="sub" style="margin-top:18px;font-size:28px">{caption}</div>' if caption else "")
        + f"</div>{tag}"
    )


def start_output():
    # the demo's port and temp folder are replaced with the defaults a reader would see
    lines = [
        "gorun is serving on http://localhost:8080 (listening on :8080, all interfaces)",
        "  Gorunfile  default, kept in the history database (version 1), no schedules",
        "  NATS       embedded server at nats://127.0.0.1:4222, events on gorun.events",
        "  history    /home/dev/.gorun/gorun.db",
        "  login      none: anyone who can open the web UI may run tasks, edit the Gorunfile and",
        "             download models (add a user on the Users page, or set GORUN_ADMIN_PASSWORD)",
        "  log        /home/dev/.gorun/gorun.log",
        "Press ctrl+c to stop.",
    ]
    return shell([("cmd", "gorun -serve")] + [("out", ln) for ln in lines], "bash")


SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("start", None, [
        (6, start_output()),
        (8, slide("No login until you add one", [
            ("warn", "Anyone who can reach the port can run tasks", "until you add a user, or set a password"),
            ("", "Add users", "on the Users page: running tasks needs one"),
            ("", "Or bind it to your own machine", "GORUN_ADDRESS=127.0.0.1:8080 gorun -serve"),
        ])),
    ]),
    ("gorunfiles", None, [
        (6, shot("config-imported", "Config: Import a Gorunfile or a zip")),
        (9, shot("gorunfiles", "Gorunfiles: several, kept in the database")),
        (6, slide("Kept in the database", [
            ("", "Share tasks", 'include = ["stored::base"]'),
            ("", "Every save is a version", "Export as zip: one file per task"),
            ("", "Only watching?", "gorun -serve -live: Live, History and Assistant"),
        ])),
    ]),
    ("users", None, [
        (5, shot("users-first", "Users: add yourself before you run anything")),
        (7, shot("users", "Admins and users, kept in the history database")),
        (6, slide("Who may do what", [
            ("", "Anyone", "looks at every page and asks the assistant"),
            ("", "A user", "also runs tasks, recorded on every run, and edits"),
            ("", "An admin", "also manages users and the assistant's models"),
        ])),
    ]),
    ("run", None, [
        (4, shot("live-empty", "Live: stats and recent runs")),
        (5, shot("run-picker", "Run: pick servers, then tasks or a bundle")),
        (4, shot("live-run", "Live: each run starts collapsed")),
        (5, shot("live-srv", "Open a server to see its tasks")),
        (6, shot("live-fail", "A failed task turns its server red")),
    ]),
    ("history", None, [
        (4, shot("history", "History: every run, newest first")),
        (5, shot("run-page", "A run: every server's tasks")),
        (7, shot("run-fail", "The failed task: exit code, error and command")),
    ]),
    ("terminal", None, [(1, slide("One place to look", [
        ("", "Runs from the web", "started on the Run page"),
        ("", "Runs from a terminal", "gorun Gorunfile -r, on the same NATS server"),
        ("", "Runs from schedules and CI", "each marked with where it came from"),
    ]))]),
    ("tasks", None, [
        (4, shot("tasks", "Tasks: filter, add and edit")),
        (7, shot("tasks-open", "A task: command, check, verify and who runs it")),
    ]),
    ("schedules", None, [(1, shot("schedules", "Schedules: pause, resume, skip and edit"))]),
    ("assistant", None, [
        (6, shot("assistant", "New: the Assistant tab")),
        (5, slide("Experimental", [
            ("", "A small model, trained on gorun", "runs on your own machine"),
            ("warn", "Needs much more training", "it can make things up: read everything it writes"),
        ])),
    ]),
    ("outro", None, [(1, slide("Recap", [
        ("", "gorun -serve", "the dashboard, the scheduler and your Gorunfiles"),
        ("", "Run, Live and History", "start, watch and look back"),
        ("", "Add users", "or keep it on a network you trust"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "start": "Starting the dashboard",
    "gorunfiles": "Gorunfiles in the database",
    "run": "Running and watching",
    "history": "History",
    "terminal": "Runs from anywhere",
    "tasks": "Tasks and bundles",
    "schedules": "Schedules",
    "assistant": "The assistant (experimental)",
    "users": "Users and sign in",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "05-web-dashboard")
