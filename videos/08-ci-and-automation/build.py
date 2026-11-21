#!/usr/bin/env python3
"""Builds video 8, "gorun in CI and automation": real runs, the web history, a workflow example, slides and a
Chatterbox voiceover -> MP4, captions and chapters.

    PW=/path/with/playwright-core ./record.sh     # captures the runs (needs Go, unshare, chromium, node)
    ./build.py                                    # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import base64
import html
import os
import sys
import textwrap
from pathlib import Path

# same pace as the other videos (~196 words/min)
os.environ.setdefault("TEMPO", "1.22")

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import code, page, shell, slide, text_file  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
GORUNFILE = (HERE / "demo" / "Gorunfile").read_text()
WORKFLOW = (HERE / "workflow.yml").read_text()


def rc(name):
    return (CAP / f"{name}.rc").read_text().strip()


def out(cmd, name, exit_code=True, keep=None, env="", lead=""):
    """A command, its captured output and (like `echo $?`) its exit code."""
    lines = []
    for ln in text_file(name):
        lines += [("out", w) for w in (textwrap.wrap(ln, 76, subsequent_indent="    ", break_long_words=False) or [""])]
    if keep:
        lines = [x for x in lines if keep(x[1])]
    rows = [("cmd", (env + " " if env else "") + cmd)] + ([("out", lead)] if lead else []) + lines
    if exit_code:
        rows += [("cmd", "echo $?"), ("out", rc(name))]
    return shell(rows)


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">gorun Gorunfile -r --plain</div>'
        "<h1>gorun in <span class=acc>CI</span> and automation</h1>"
        '<div class="sub">Pipelines, scripts and cron.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 8 of the series</div></div>'
    )


def table_slide(title, rows):
    body = "".join(f'<tr><td class="mono acc" style="white-space:nowrap">{html.escape(a)}</td><td>{html.escape(b)}</td></tr>' for a, b in rows)
    return page(f'<div class="wrap"><h2>{title}</h2><table style="font-size:34px">{body}</table></div><div class="brand mono">gorun</div>',
                "td{padding:16px 26px}")


def shot(name, caption="", badge=""):
    b64 = base64.b64encode((CAP / f"{name}.png").read_bytes()).decode()
    tag = f'<div class="badge">{badge}</div>' if badge else ""
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center;padding:30px">'
        f'<img style="max-height:930px;max-width:1860px;border-radius:14px;border:1px solid var(--line)" src="data:image/png;base64,{b64}">'
        + (f'<div class="sub" style="margin-top:18px;font-size:28px">{caption}</div>' if caption else "")
        + f"</div>{tag}"
    )


def block(text, start):
    """Line range [start, end) of the blank-line separated block that begins with `start`."""
    lines = text.split("\n")
    i = next(n for n, ln in enumerate(lines) if ln.startswith(start))
    j = i
    while j < len(lines) and lines[j].strip() != "":
        j += 1
    return i, j


def yaml(start, end, title="deploy.yml"):
    return code(WORKFLOW, start, end, title, lang="yaml", window_lines=26, font=26)


w = WORKFLOW.split("\n")
i_dry = next(i for i, ln in enumerate(w) if "Install gorun" in ln) - 1
i_dep = next(i for i, ln in enumerate(w) if "- name: Dry run" in ln)
i_dpl = next(i for i, ln in enumerate(w) if "- name: Deploy" in ln)
d0, d1 = block(GORUNFILE, "[tasks.deploy]")
CI = "Simulated CI variables"

SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("nopicker", None, [
        (5, out("gorun Gorunfile", "nopicker")),
        (7, out("gorun Gorunfile -r --plain", "run-ok")),
    ]),
    ("failing", None, [
        (8, out("gorun Gorunfile -r --plain", "run-fail", env="SMOKE_FAIL=1",
                keep=lambda t: " -> " not in t and "config ok" not in t and "migrate ok" not in t, lead="…")),
        (5, table_slide("Exit codes", [("0", "every task passed"), ("1", "a task failed, or gorun could not run"),
                                       ("130", "ctrl+c pressed twice: quit at once")])),
    ]),
    ("aim", None, [
        (4, table_slide("Aim a run", [("--server, --exclude-server", "names or globs"), ("--tag, --group", "servers by label"),
                                      ("--task, --bundles", "one task, a glob or a bundle"), ("--parallel N", "at most N servers at a time")])),
        (5, out("gorun Gorunfile --list-servers --tag prod --plain", "list-prod", exit_code=False)),
        (5, out("gorun Gorunfile -r --plain --tag staging", "run-staging")),
    ]),
    ("check", None, [
        (6, out("gorun Gorunfile -r --plain --check", "check-before", keep=lambda t: "would run (" not in t or "migrate" in t or "check only" in t)),
        (6, out("gorun Gorunfile -r --plain --check", "check-after", keep=lambda t: "would run (" not in t or "migrate" in t or "check only" in t)),
    ]),
    ("secrets", None, [
        (5, code(GORUNFILE, d0, d1, "Gorunfile", window_lines=24, font=30)),
        (5, shell([("cmd", "grep -c \"$DEPLOY_TOKEN\" ~/.gorun/gorun.log"), ("out", text_file("token-in-log")[0])])),
    ]),
    ("origin", None, [
        (5, shot("history", "History: the run marked github-actions · octocat", CI)),
        (8, shot("run-origin", "Who and where: the system, the user, the labels", CI)),
    ]),
    ("workflow", None, [
        (3, yaml(0, i_dry, "deploy.yml  (an example)")),
        (4, yaml(i_dry, i_dep, "deploy.yml  (an example)")),
        (4, yaml(i_dep, i_dpl, "deploy.yml  (an example)")),
        (6, yaml(i_dpl, len(w) - 1, "deploy.yml  (an example)")),
    ]),
    ("cron", None, [
        (6, code("# nightly, from cron\n0 2 * * *  cd /srv/deploys && gorun Gorunfile -r --plain --tag staging\n\n"
                 "# or keep the service up, and use [[schedule]] in the Gorunfile\ngorun -serve Gorunfile",
                 0, 5, "crontab  (an example)", lang="sh", window_lines=8, font=30)),
    ]),
    ("outro", None, [(1, slide("Recap", [
        ("", "-r --plain", "no picker, plain output"),
        ("", "Exit code and filters", "fail the job, aim the run, preview with --check"),
        ("", "$VAR and labels", "secrets from the environment, who ran what recorded"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "nopicker": "No picker",
    "failing": "Failing a pipeline",
    "aim": "Aiming a run",
    "check": "Checking first",
    "secrets": "Secrets",
    "origin": "Who ran it",
    "workflow": "A GitHub Actions workflow",
    "cron": "Cron and schedules",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "08-ci-and-automation")
