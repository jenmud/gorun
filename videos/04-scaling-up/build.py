#!/usr/bin/env python3
"""Builds video 4, "Scaling up: include, shared task libraries and schedules": slides, code, gorun output and a
Chatterbox voiceover -> MP4, captions and chapters.

    GORUN_SRC=/path/to/gorun ./record.sh     # capture the gorun output (needs Go, git, unshare, chromium)
    ./build.py                               # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import base64
import html
import os
import re
import sys
import textwrap
from pathlib import Path

# same pace as the other videos (~196 words/min)
os.environ.setdefault("TEMPO", "1.22")

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import block, code, page, shell, slide, text_file  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
S = HERE / "steps"
ROOT = (S / "split" / "Gorunfile").read_text()
GROUP = (S / "split" / "groups" / "web.toml").read_text()
HOSTS = (S / "split" / "hosts" / "web.toml").read_text()
LIB = (S / "library" / "web" / "base.toml").read_text()
GITINC = (S / "gitinc" / "Gorunfile").read_text()
SCHED = (S / "schedules" / "Gorunfile").read_text()


def tidy(ln):
    # the throwaway cache path of the run that was captured
    ln = re.sub(r"(\.\./)+tmp/tmp\.[^/]+/cache/git/[0-9a-f]+/[0-9a-f]+/", "~/.cache/gorun/", ln)
    return re.sub(r"\(logs: [^)]*\)", "(logs: ~/.gorun/gorun.log)", ln)


def out_lines(name):
    rows = []
    for ln in text_file(name):
        # long error messages wrap, instead of running off the frame
        wrapped = textwrap.wrap(tidy(ln), 74, subsequent_indent="    ", break_long_words=False) or [""]
        rows += [("out", w) for w in wrapped]
    return rows


def run_output(cmd, name, title="bash"):
    return shell([("cmd", cmd)] + out_lines(name), title)


def two(cmd1, name1, cmd2, name2, title="bash"):
    return shell([("cmd", cmd1)] + out_lines(name1) + [("cmd", cmd2)] + out_lines(name2), title)


def whole(text, title):
    return code(text, 0, len(text.rstrip("\n").split("\n")), title)


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">include = [ "tasks/*.toml" ]</div>'
        "<h1>Scaling <span class=acc>up</span></h1>"
        '<div class="sub">Include files, shared tasks and schedules.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 4 of the series</div></div>'
    )


def table_slide(title, rows, head=None):
    th = f"<tr>{''.join(f'<th>{h}</th>' for h in head)}</tr>" if head else ""
    body = "".join(f'<tr><td class="mono acc" style="white-space:nowrap">{html.escape(a)}</td><td>{html.escape(b)}</td></tr>' for a, b in rows)
    return page(f'<div class="wrap"><h2>{title}</h2><table style="font-size:34px">{th}{body}</table></div><div class="brand mono">gorun</div>',
                "td{padding:16px 26px}")


def tree_slide(title, name):
    return shell([("cmd", "find . -type f | sort")] + [("out", ln) for ln in text_file(name)], title)


def screenshot(name, caption):
    b64 = base64.b64encode((CAP / f"{name}.png").read_bytes()).decode()
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;align-items:center;justify-content:center">'
        f'<img style="width:1700px;border-radius:14px;border:1px solid var(--line)" src="data:image/png;base64,{b64}">'
        f'<div class="sub" style="margin-top:28px;font-size:30px">{caption}</div></div>'
    )



nl = SCHED.split("\n")
i_n = next(i for i, ln in enumerate(nl) if ln.strip() == 'name = "nightly"') - 1
i_m = next(i for i, ln in enumerate(nl) if ln.strip() == 'name = "maintenance"') - 1

SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("problem", None, [(1, slide("When a Gorunfile grows", [
        ("bad", "One huge file", "and finding anything means scrolling"),
        ("bad", "The same tasks copied between projects", "and the copies drift apart"),
        ("bad", "Jobs that need to run every night", "but depend on someone remembering"),
    ]))]),
    ("split1", None, [
        (4, whole(ROOT, "Gorunfile")),
        (4, tree_slide("The fleet, split across files", "split-tree")),
    ]),
    ("split2", None, [
        (3, whole(GROUP, "groups/web.toml")),
        (3, whole(HOSTS, "hosts/web.toml")),
        (5, run_output("gorun Gorunfile --list-servers --plain", "split-servers")),
    ]),
    ("splitrules", None, [
        (5, run_output("gorun Gorunfile --run --plain", "dup-error")),
        (6, table_slide("Include rules", [
            ("one name, one place", "a name defined twice is an error that names both files"),
            ("root-only settings", "title, logging and SSH defaults live in the root file"),
            ("no match, no loops", "a pattern with no files, or a loop, is an error"),
            ("paths", "relative to the root Gorunfile"),
        ])),
    ]),
    ("gitintro", None, [
        (4, tree_slide("task-library: a repository of TOML files", "lib-tree")),
        (4, whole(LIB, "web/base.toml")),
        (8, whole(GITINC, "Gorunfile")),
    ]),
    ("gitrun", None, [
        (5, run_output("gorun Gorunfile --run --plain", "git-run1")),
        (3, shell([("cmd", "cat Gorunfile.lock")] + [("out", ln) for ln in text_file("git-lock")], "Gorunfile.lock  (commit it)")),
        (3, run_output("gorun Gorunfile --list-bundles --plain", "git-list-before")),
        (4, two("gorun Gorunfile --update-includes", "git-update", "gorun Gorunfile --list-bundles --plain", "git-list-after")),
        (4, run_output("gorun Gorunfile --run --plain", "git-run2")),
    ]),
    ("gitsafe", None, [
        (6, run_output("gorun Gorunfile --run --plain", "git-bad")),
        (8, slide("A library can only add tasks", [
            ("", "Tasks and bundles only", "no servers, groups or schedules"),
            ("", "Pinned by the lock file", "until you run --update-includes"),
            ("", "Credentials are git's own", "nothing secret in the Gorunfile"),
            ("", "Recorded with every run", "the ref and the commit that ran"),
        ])),
    ]),
    ("schedintro", None, [
        (6, code(SCHED, i_n, i_n + 7, "Gorunfile")),
        (4, table_slide("Cron: five fields", [
            ("0  2  *  *  *", "minute  hour  day  month  weekday"),
            ("*/15 * * * *", "every 15 minutes"),
            ("0 2 * * 1-5", "02:00, Monday to Friday"),
        ])),
        (4, code(SCHED, i_m, i_m + 6, "Gorunfile")),
    ]),
    ("schedrun", None, [
        (8, slide("Schedules need the service", [
            ("", "gorun -serve Gorunfile", "starts the scheduler and the web dashboard"),
            ("", "Not running, nothing fires", "a plain gorun -r never fires schedules"),
            ("", "Keep it up", "on a small server, or as a system service"),
        ])),
        (8, screenshot("web-schedules", "gorun -serve: the Schedules page")),
        (6, run_output("gorun Gorunfile --run --plain", "sched-bad")),
    ]),
    ("outro", None, [(1, slide("Recap", [
        ("", "include", "split a big Gorunfile across files"),
        ("", "git::…?ref=", "shared tasks, pinned by a lock file"),
        ("", "[[schedule]]", "cron patterns and one-off times"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "problem": "When a Gorunfile grows",
    "split1": "Splitting with include",
    "splitrules": "Include rules",
    "gitintro": "Shared tasks from git",
    "gitrun": "The lock file and updating",
    "gitsafe": "Why it's safe",
    "schedintro": "Schedules",
    "schedrun": "The Schedules page",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "04-scaling-up")
