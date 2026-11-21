#!/usr/bin/env python3
"""Builds video 2, "Your first Gorunfile, and the terminal interface": slides, code, gorun screens
and a Chatterbox voiceover -> MP4, captions and chapters.

    GORUN_SRC=/path/to/gorun ./record.sh     # capture the gorun screens (needs tmux, Go)
    ./build.py                               # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import html
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import code, page, shell, slide, window  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
ANSI = re.compile(r"\x1b\[[0-9;]*m")
DEMO = (HERE / "demo" / "Gorunfile").read_text()
STEP1 = (HERE / "steps" / "step1" / "Gorunfile").read_text()
STEP2 = (HERE / "steps" / "step2" / "Gorunfile").read_text()
IPS = {"db-1": "10.0.0.21", **{f"web-{n}": f"10.0.0.1{n}" for n in range(1, 5)}}


def block(marker, text):
    """Line range [start, end) of the blank-line separated block that begins at `marker`."""
    lines = text.split("\n")
    i = next(n for n, ln in enumerate(lines) if ln.startswith(marker))
    j = i
    while j < len(lines) and lines[j].strip() != "":
        j += 1
    return i, j


def tmux(name, badge="", title="gorun"):
    # the runs are on local stand-ins; show the addresses the demo fleet has
    def relabel(text):
        out = []
        for ln in text.split("\n"):
            m = re.search(r"\b(web-\d|db-1)\b", ANSI.sub("", ln))
            out.append(ln.replace("local", IPS[m.group(1)], 1) if m else ln)
        return "\n".join(out)

    rows = len((CAP / f"{name}.txt").read_text().rstrip("\n").split("\n"))
    big = rows > 28  # tall screens get a smaller font so the whole window fits
    return common.tmux(name, badge, title, relabel, minw=1380 if big else 1560, font=22 if big else 26, lh=1.22 if big else 1.25)


def text_file(name, skip=0, take=None):
    lines = ANSI.sub("", (CAP / f"{name}.txt").read_text()).rstrip("\n").split("\n")
    return lines[skip:take]


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">$ gorun Gorunfile</div>'
        "<h1>Your first <span class=acc>Gorunfile</span></h1>"
        '<div class="sub">Tasks, servers and groups, then the picker,<br>the live view and the history.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 2 of the series</div></div>'
    )


def concept_cards(n):
    data = [
        ("task", "a shell command with a name", '[tasks.hello]\ncmd = "echo hi"'),
        ("server", "a machine to run it on", '[servers.hosts.web-1]\nhostname = "10.0.0.11"'),
        ("group", "what servers share", '[server_groups.ubuntu]\n  [...ssh]\n  username = "ubuntu"'),
        ("tag", "a label to pick servers by", 'tags = ["prod"]'),
        ("bundle", "a name for a list of tasks", '[bundles.webapp]\ntasks = ["nginx",\n  "site", "smoke"]'),
    ]
    cards = []
    for i, (name, sub, ex) in enumerate(data):
        op = "1" if i < n else ".12"
        cur = "border-color:var(--acc)" if i == n - 1 else ""
        cards.append(
            f'<div style="opacity:{op};background:var(--card);border:3px solid var(--line);{cur};border-radius:22px;'
            f'padding:30px 20px;width:316px;height:470px"><div style="font-size:46px;font-weight:700" class="acc mono">{name}</div>'
            f'<div class="dim" style="font-size:28px;margin:8px 0 30px">{sub}</div>'
            f'<div class="mono" style="font-size:19px;line-height:1.6;white-space:pre;color:#cbd5e1">{html.escape(ex)}</div></div>'
        )
    return page(
        '<div class="wrap"><h2>Five words are all you need</h2>'
        '<div style="display:flex;gap:22px;margin-top:50px">' + "".join(cards) + "</div></div>"
        '<div class="brand mono">gorun</div>'
    )


def keys_slide():
    rows = [
        ("space", "select a server, tick a task, check a bundle"),
        ("/", "filter: type, enter keeps it, esc clears it"),
        ("j / k  ·  ↓ / ↑", "move"),
        ("l / h  ·  → / ←", "show or hide details"),
        ("tab", "switch between servers and tasks"),
        ("alt+s", "select all  (alt+d: deselect all)"),
        ("alt+a", "select every server and run"),
        ("1 – 5", "jump to a tab"),
        ("enter", "run  ·  in the live view: open"),
        ("v", "live view: verbose or compact"),
        ("?", "every shortcut   ·   q: quit"),
    ]
    body = "".join(f'<tr><td class="mono acc" style="white-space:nowrap">{html.escape(k)}</td><td>{html.escape(d)}</td></tr>' for k, d in rows)
    return page(
        f'<div class="wrap"><h2>Keys for the terminal interface</h2><table style="font-size:30px">{body}</table></div>'
        '<div class="brand mono">gorun</div>',
        "td{padding:11px 26px} .wrap{padding-top:60px} h2{margin-bottom:24px}",
    )


def commands_slide():
    lines = [
        ("cmd", "gorun Gorunfile --run                       # no picker"),
        ("cmd", "gorun Gorunfile -r --plain --tag prod       # plain text, for CI"),
        ("cmd", "gorun Gorunfile -r --server 'web-*'         # servers, with globs"),
        ("cmd", "gorun Gorunfile -r --group web --tag prod   # all of these must match"),
        ("cmd", "gorun Gorunfile -r --task @webapp           # a bundle, or a task"),
        ("cmd", "gorun -history                              # past runs"),
    ]
    return shell(lines, "Without the picker")


def keep(lines, title, n=None):
    return shell([("cmd", t) if k == "cmd" else ("out", t) for k, t in lines[: n or len(lines)]], title)


# --- ranges in the files shown on screen -----------------------------------------------------

S1_TASK = block("[tasks.hello]", STEP1)
S1_SERVERS = block("[servers]", STEP1)
S1_HOST = block("[servers.hosts.local]", STEP1)
S2_MAKE, S2_WRITE = block("[tasks.make-dir]", STEP2), block("[tasks.write-file]", STEP2)
S2_SERVERS = block("[servers]", STEP2)
UBUNTU = block("[server_groups.ubuntu]", DEMO)
WEB1 = block("[servers.hosts.web-1]", DEMO)
WEB4 = block("[servers.hosts.web-4]", DEMO)
UPDATE, SITE = block("[tasks.update]", DEMO), block("[tasks.site]", DEMO)
BUNDLE = block("[bundles.webapp]", DEMO)
GWEB, GDB = block("[server_groups.web]", DEMO), block("[server_groups.db]", DEMO)
GPROD, GSTAGING = block("[server_groups.prod]", DEMO), block("[server_groups.staging]", DEMO)

BADGE = "Demo run, sped up"
live = lambda name: tmux(name, badge=BADGE)  # noqa: E731

# Each scene: id, narration (None: looked up in narration.py), and shots (weight, html).
SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("concepts", None, [(1, concept_cards(i)) for i in range(1, 6)]),
    ("step1", None, [
        (3, code(STEP1, 0, S1_TASK[1], "Gorunfile")),
        (2, code(STEP1, S1_SERVERS[0], S1_SERVERS[1], "Gorunfile")),
        (2, code(STEP1, S1_HOST[0], S1_HOST[1], "Gorunfile")),
    ]),
    ("step1run", None, [
        (2, tmux("s1-picker")), (2, tmux("s1-picked")), (1, tmux("s1-done")),
        (3, keep([("cmd", "gorun Gorunfile --run --plain -v")] + [("out", ln) for ln in text_file("s1-plain")], "bash")),
    ]),
    ("step2", None, [
        (3, code(STEP2, S2_MAKE[0], S2_WRITE[1], "Gorunfile")),
        (2, code(STEP2, S2_SERVERS[0], S2_SERVERS[1], "Gorunfile")),
        (2, keep([("cmd", "gorun Gorunfile --list-tasks")] + [("out", ln) for ln in text_file("s2-tasks")], "bash")),
        (2, keep([("cmd", "gorun Gorunfile --list-servers")] + [("out", ln) for ln in text_file("s2-servers")], "bash")),
    ]),
    ("step3", None, [
        (3, code(DEMO, UBUNTU[0], UBUNTU[1], "Gorunfile")),
        (4, code(DEMO, WEB1[0], WEB1[1], "Gorunfile")),
    ]),
    ("step4", None, [
        (4, code(DEMO, UPDATE[0], UPDATE[1], "Gorunfile")),
        (5, code(DEMO, SITE[0], SITE[1], "Gorunfile")),
    ]),
    ("step5", None, [
        (3, code(DEMO, BUNDLE[0], BUNDLE[1], "Gorunfile")),
        (3, code(DEMO, GWEB[0], GDB[1], "Gorunfile")),
        (2, code(DEMO, GPROD[0], GSTAGING[1], "Gorunfile")),
        (3, code(DEMO, WEB4[0], WEB4[1], "Gorunfile")),
    ]),
    ("validate", None, [
        (1, keep([("cmd", "gorun Gorunfile --list-servers")] + [("out", ln) for ln in text_file("list-servers", 0, 17)] + [("out", "…")], "bash")),
    ]),
    ("picker", None, [(1, tmux("picker"))]),
    ("select", None, [(2, tmux("picker-groups-web")), (2, tmux("picker-filter")), (2, tmux("picker-selected"))]),
    ("tasks", None, [(3, tmux("picker-tasks")), (2, tmux("picker-details"))]),
    ("tabs", None, [(2, tmux("tab-tasks")), (3, tmux("tab-bundles")), (2, tmux("tab-groups"))]),
    ("run", None, [(2, live("live-compact")), (2, live("live-expanded")), (2, live("live-verbose")), (2, live("live-output"))]),
    ("fail", None, [(3, live("live-failed")), (2, live("live-verbose-failed"))]),
    ("history", None, [(2, tmux("history")), (1, tmux("history-run")), (1, tmux("history-server")), (2, tmux("history-task"))]),
    ("scripts", None, [(1, commands_slide())]),
    ("cheat", None, [(1, keys_slide())]),
    ("outro", None, [(1, slide("Next time", [
        ("", "Checks that skip work that's already done", "and verifies that prove a change took"),
        ("", "Secrets that stay out of your Gorunfile", "read from your environment"),
        ("", "Templates", "every server gets its own settings"),
    ]))]),
]

# Chapter starts for the video page and YouTube: scene id -> title. Scenes not listed belong to the previous chapter.
CHAPTERS = {
    "intro": "Welcome",
    "concepts": "The five concepts",
    "step1": "Step 1: your first Gorunfile",
    "step2": "Step 2: more tasks, in order",
    "step3": "Step 3: real servers and groups",
    "step4": "Step 4: sudo, retries and copies",
    "step5": "Step 5: bundles, groups and tags",
    "validate": "Check before you run",
    "picker": "The picker",
    "run": "The live view",
    "history": "History",
    "scripts": "Scripts and CI",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "02-gorunfile-and-tui")
