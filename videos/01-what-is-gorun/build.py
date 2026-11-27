#!/usr/bin/env python3
"""Builds the "What is gorun?" video: slides + terminal frames + a Chatterbox voiceover -> MP4 and SRT.

    GORUN_SRC=/path/to/gorun ./record.sh     # capture the gorun screens (needs tmux, Go)
    ./build.py                               # Chatterbox voice (needs a CUDA GPU)
    ENGINE=kokoro KOKORO=/path/to/kokoro-v1.0.onnx ./build.py   # lighter fallback

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

import html
import os
import re
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import common  # noqa: E402
from common import code, page, shell, slide, window  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
GORUN = os.environ.get("GORUN", "gorun")


def tmux(name, badge="", title="gorun"):
    # the live run is on local stand-ins; show the addresses the real servers would have
    def relabel(text):
        return "\n".join(
            re.sub(r"local\b", f"10.0.0.1{m.group(1)}", ln) if (m := re.search(r"node-(\d)", ln)) else ln
            for ln in text.split("\n")
        )

    return common.tmux(name, badge, title, relabel)


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">$ gorun Gorunfile --run</div>'
        "<h1>What is <span class=acc>gorun</span>?</h1>"
        '<div class="sub">Run plain shell commands on a pile of servers,<br>in parallel, and watch it happen.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 1 of the series</div></div>'
    )


def cards(n_visible):
    data = [
        ("tasks", "the commands", '[tasks.docker-install]\ncmd = "…"\nsudo = true\ndepends_on = […]'),
        ("bundles", "names for lists of tasks", '[bundles.docker]\ntasks = [\n  "docker-install",\n  "docker-group",\n]'),
        ("server_groups", "what servers share", '[server_groups.ubuntu]\ntasks = ["@docker", …]\n[…ssh]\nusername = "ubuntu"'),
        ("servers", "the machines", '[servers.hosts.node-1]\nhostname = "10.0.0.11"\ngroups = ["ubuntu"]'),
    ]
    out = []
    for i, (name, sub, ex) in enumerate(data):
        op = "1" if i < n_visible else ".12"
        cur = "border-color:var(--acc)" if i == n_visible - 1 else ""
        out.append(
            f'<div style="opacity:{op};background:var(--card);border:3px solid var(--line);{cur};border-radius:22px;'
            f'padding:40px 34px;width:390px;height:520px"><div style="font-size:38px;font-weight:700" class="acc mono">{name}</div>'
            f'<div class="dim" style="font-size:30px;margin:8px 0 34px">{sub}</div>'
            f'<div class="mono" style="font-size:24px;line-height:1.55;white-space:pre;color:#cbd5e1">{html.escape(ex)}</div></div>'
        )
    return page(
        '<div class="wrap"><h2>The Gorunfile: four kinds of section</h2>'
        '<div style="display:flex;gap:28px;margin-top:60px">' + "".join(out) + "</div></div>"
        '<div class="brand mono">gorun</div>'
    )


def compare():
    rows = [
        ("Mental model", "Run these commands over there", "Make the fleet look like this"),
        ("A task is…", "A shell command you already know", "A module call you look up"),
        ("On the server", "SSH. Nothing else.", "SSH and Python"),
        ("Re-run safety", "Per-task checks you write", "Idempotent modules"),
        ("Best for", "Hands-on jobs, a handful to a few dozen servers", "Long-lived fleets, drift control"),
    ]
    body = "".join(f"<tr><td class=dim>{a}</td><td>{b}</td><td>{c}</td></tr>" for a, b, c in rows)
    return page(
        '<div class="wrap"><h2>gorun and Ansible: different jobs</h2><table>'
        '<tr><th></th><th class=acc>gorun</th><th style="color:var(--acc2)">Ansible</th></tr>'
        f"{body}</table></div>"
    )


def browser(host, ip):
    def run(cmd):
        return subprocess.run(cmd, shell=True, capture_output=True, text=True).stdout.rstrip()

    stats = f"{run('uptime')}\n\n{run('free -m')}\n\n{run('df -h /')}"
    return page(
        '<div class="wrap" style="display:flex;align-items:center;justify-content:center;padding:50px">'
        '<div class="chrome" style="width:1500px;background:#fff"><div class="bar"><i style="background:#f87171"></i>'
        '<i style="background:#fbbf24"></i><i style="background:#4ade80"></i>'
        f'<span style="margin-left:18px;background:#0f1117;padding:8px 26px;border-radius:999px;flex:1">http://{ip}</span></div>'
        f'<div style="background:#fff;color:#111;padding:40px 60px;height:700px"><h1 style="font-size:70px;margin:0 0 24px">{host}</h1>'
        f'<pre style="font:28px/1.4 monospace;margin:0">{html.escape(stats)}</pre></div></div></div>'
    )


def dump_head(n=14):
    try:
        out = subprocess.run([GORUN, "-dump"], capture_output=True, text=True, timeout=20).stdout
    except OSError:
        out = ""
    return "\n".join(out.split("\n")[:n])


# --- the script -------------------------------------------------------------------------------

DEMO = (HERE / "demo" / "Gorunfile").read_text()
COMPOSE = (HERE / "demo" / "files" / "docker-compose.yml").read_text()


def block(marker, text=DEMO):
    """Line range [start, end) of the blank-line separated block that begins at `marker`."""
    lines = text.split("\n")
    i = next(n for n, ln in enumerate(lines) if ln.startswith(marker))
    j = i
    while j < len(lines) and lines[j].strip() != "":
        j += 1
    return i, j


T_END = block("[tasks.docker-install]")[1]
G_START, G_END = block("[tasks.docker-group]")
F_START = block("[tasks.app-dir]")[0]
CU_START, F_END = block("[tasks.compose-up]")
B_START = block("[bundles.docker]")[0]
S_END = block("[servers.hosts.node-2]")[1]
FILE_END = len(DEMO.rstrip("\n").split("\n"))

# Each scene: id, narration (None: looked up in narration.py), and shots.
# A shot is (weight, html); a scene's audio is split between its shots by weight.
SCENES = [
    ("intro", None,
     [(1, title_slide())]),

    ("voice", None,
     [(1, slide("A quick note on the voice", [
         ("", "The narration is a synthetic voice", "so tutorials can be re-recorded whenever gorun changes"),
         ("", "What's on screen is real", "the Gorunfile and commands are in the repo, try them yourself"),
         ("", "Prefer reading?", "captions are on, and docs/tutorial.md covers the same ground"),
     ]))]),

    ("what", None,
     [(1, slide("One binary. One file. <span class=acc>SSH.</span>", [
         ("", "A single Go program", "a web dashboard, a terminal UI and CI"),
         ("", "Gorunfiles kept in its database", "every version, or as plain files"),
         ("", "Plain shell commands, in parallel", "every server at the same time"),
         ("", "Nothing to install on the servers", "no agents, no daemons, no Python"),
     ]))]),

    ("why", None,
     [(1, slide("Why you might like it", [
         ("", "Tasks are plain commands", "copy one, paste it, debug it by hand"),
         ("", "Parallel by default", "a failure stops that server, not the rest"),
         ("", "Nothing runs by accident", "pick servers and tasks first, every task has a timeout"),
         ("", "It remembers", "every run saved, with commands and output"),
     ]))]),

    ("ansible", None,
     [(1, compare()),
      (2, slide("Why gorun exists", [
          ("", "You stay in control", "you decide what runs on the server and how, no hidden abstractions"),
          ("", "Errors you can act on", "see the original error, copy the command, run it by hand on the server"),
          ("", "Nothing to keep updated", "no Python, modules or virtual environments"),
      ])),
      (1, compare())]),

    ("notgood", None,
     [(1, slide("What it's <span style='color:var(--bad)'>not</span> good for", [
         ("bad", "It runs commands, it doesn't enforce state", ""),
         ("bad", "No modules, facts or automatic drift detection", ""),
         ("bad", "No dynamic inventories or secrets vault", ""),
         ("", "Checks, not enforcement", "a task's check skips it when there's nothing to do; -check is a dry run, and checks must only look"),
         ("warn", "Big fleets that must stay converged?", "use Ansible, Salt, Puppet or Nix"),
     ]))]),

    ("install", None,
     [(3, shell([("cmd", "curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | sh")],
                "Install")),
      (3, shell([("cmd", "gorun -dump > Gorunfile"), ("cmd", "head -14 Gorunfile")] +
                [("out", ln) for ln in dump_head()[:2000].split("\n")], "Quick start"))]),

    ("concepts", None,
     [(1, cards(1)), (1, cards(2)), (1, cards(3)), (1, cards(4))]),

    ("goal", None,
     [(1, slide("The goal", [
         ("", "5 fresh Ubuntu servers", "SSH key login, passwordless sudo"),
         ("", "Docker installed, user in the docker group", ""),
         ("", "docker-compose.yml: Caddy serving a resource page", "hostname, uptime, memory and disk"),
     ]))]),

    ("task1", None,
     [(1, code(DEMO, 0, T_END, "Gorunfile"))]),

    ("task2", None,
     [(1, code(DEMO, G_START, G_END, "Gorunfile"))]),

    ("files", None,
     [(3, code(DEMO, F_START, CU_START - 1, "Gorunfile")),
      (3, code(COMPOSE, 0, len(COMPOSE.rstrip().split("\n")), "files/docker-compose.yml", lang="yaml", window_lines=27, font=25)),
      (3, code(DEMO, CU_START, F_END, "Gorunfile", font=27))]),

    ("wire", None,
     [(1, code(DEMO, B_START, FILE_END, "Gorunfile"))]),

    ("list", None,
     [(1, shell([("cmd", "gorun Gorunfile --list-servers")] + [("out", ln) for ln in [
         "node-1 (10.0.0.11:22)", "    groups: ubuntu", "    ├── docker-install (retries: 2)", "    ├── docker-group",
         "    ├── app-dir", "    ├── compose-file", "    └── compose-up", "", "node-2 (10.0.0.12:22)", "    groups: ubuntu", "    ├── …"]],
        "Check what will run"))]),

    ("dryrun", None,
     [(1, tmux("check-fresh", badge="Demo run (check only)"))]),

    ("picker", None,
     [(2, tmux("picker")), (2, tmux("picker-all"))]),

    ("live", None,
     [(1, tmux("live-2", badge="Demo run, sped up")), (1, tmux("live-4", badge="Demo run, sped up")),
      (1, tmux("live-7", badge="Demo run, sped up")), (1, tmux("live-10", badge="Demo run, sped up")),
      (1, tmux("live-14", badge="Demo run, sped up")), (2, tmux("live-done", badge="Demo run, sped up"))]),

    ("result", None,
     [(1, browser("node-3", "10.0.0.13"))]),

    ("rerun", None,
     [(2, tmux("rerun-done", badge="Demo run, second time")), (1, tmux("check-done", badge="Demo run (check only)"))]),

    ("outro", None,
     [(1, slide("Try it", [
         ("", "curl -fsSL …/install.sh | sh", "install"),
         ("", "gorun -dump > Gorunfile", "start from the documented file"),
         ("", "github.com/jenmud/gorun", "docs and the full tutorial"),
     ]))]),
]

# Chapter starts for the video page and YouTube: scene id -> title. Scenes not listed belong to the previous chapter.
CHAPTERS = {
    "intro": "Welcome",
    "what": "What is gorun?",
    "why": "Why use it, and gorun vs Ansible",
    "notgood": "What it's not good for",
    "install": "Installing",
    "concepts": "The Gorunfile and its sections",
    "goal": "Demo: five new Ubuntu servers",
    "dryrun": "A dry run with -check",
    "picker": "The picker and the live run",
    "rerun": "Running it again",
    "outro": "Wrap up",
}


if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "01-what-is-gorun")
