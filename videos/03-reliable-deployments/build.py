#!/usr/bin/env python3
"""Builds video 3, "Make it reliable: checks, secrets and templates": slides, code, gorun output and a
Chatterbox voiceover -> MP4, captions and chapters.

    GORUN_SRC=/path/to/gorun ./record.sh     # capture the gorun output (needs tmux, Go, unshare)
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
from common import block, code, page, shell, slide, text_file  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
DEMO = (HERE / "demo" / "Gorunfile").read_text()
CHECKS = (HERE / "steps" / "checks" / "Gorunfile").read_text()
VERIFY = (HERE / "steps" / "verify" / "Gorunfile").read_text()
SECRETS = (HERE / "steps" / "secrets" / "Gorunfile").read_text()
TEMPLATES = (HERE / "steps" / "templates" / "Gorunfile").read_text()
TMPL_FILE = (HERE / "steps" / "templates" / "app.conf.tmpl").read_text()
IPS = {f"web-{n}": f"10.0.0.1{n}" for n in range(1, 5)}
BADGE = "Demo run, sped up"


def tmux(name, badge=BADGE, title="gorun"):
    # the fleet runs on local stand-ins; show the addresses the demo fleet has
    def relabel(text):
        out = []
        for ln in text.split("\n"):
            m = re.search(r"\b(web-\d)\b", common.ANSI.sub("", ln))
            out.append(ln.replace("local", IPS[m.group(1)], 1) if m else ln)
        return "\n".join(out)

    rows = len((CAP / f"{name}.txt").read_text().rstrip("\n").split("\n"))
    big = rows > 28
    return common.tmux(name, badge, title, relabel, minw=1380 if big else 1560, font=22 if big else 26, lh=1.22 if big else 1.25)


def run_output(cmd, name, title="bash", skip=0, take=None):
    """A typed command and what gorun really printed for it."""
    lines = [re.sub(r"\(logs: [^)]*\)", "(logs: ~/.gorun/gorun.log)", ln) for ln in text_file(name)[skip:take]]
    return shell([("cmd", cmd)] + [("out", ln) for ln in lines], title)


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">$ gorun Gorunfile --check</div>'
        "<h1>Make it <span class=acc>reliable</span></h1>"
        '<div class="sub">Checks, secrets and templates.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 3 of the series</div></div>'
    )


def table_slide(title, rows, head=None, mono_first=True):
    th = f"<tr>{''.join(f'<th>{h}</th>' for h in head)}</tr>" if head else ""
    body = "".join(
        f'<tr><td class="{"mono acc" if mono_first else ""}" style="white-space:nowrap">{html.escape(a)}</td><td>{html.escape(b)}</td></tr>'
        for a, b in rows
    )
    return page(f'<div class="wrap"><h2>{title}</h2><table style="font-size:34px">{th}{body}</table></div><div class="brand mono">gorun</div>',
                "td{padding:16px 26px}")


# --- ranges in the files shown on screen -----------------------------------------------------

CFG = block("[tasks.config]", CHECKS)
VCFG = block("[tasks.config]", VERIFY)
NGINX, SITE = block("[tasks.nginx]", DEMO), block("[tasks.site]", DEMO)
UPDATE, SMOKE = block("[tasks.update]", DEMO), block("[tasks.smoke]", DEMO)
ANNOUNCE = block("[tasks.announce]", DEMO)
GLOBAL = block("envvars = { APP_ENV", DEMO)
WEB4 = block("[servers.hosts.web-4]", DEMO)
SEC_TASK = block("[tasks.login]", SECRETS)
T_HELLO, T_CONFIG = block("[tasks.hello]", TEMPLATES), block("[tasks.config]", TEMPLATES)
T_SERVERS = block("[servers.hosts.web-1]", TEMPLATES)
T_STAGING = block("[servers.hosts.staging]", TEMPLATES)
TOP = len(TEMPLATES.split("\n"))

SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("problems", None, [(1, slide("What goes wrong in real life", [
        ("bad", "A rerun repeats everything", "even the work that was already done"),
        ("bad", "A command can finish without working", "exit code 0 isn't proof"),
        ("bad", "Secrets end up in the file", "and then in git"),
        ("bad", "No two servers are quite alike", "staging is not production"),
    ]))]),
    ("checkintro", None, [
        (3, code(CHECKS, CFG[0], CFG[1] - 1, "Gorunfile")),
        (2, code(CHECKS, CFG[0] + 2, CFG[0] + 3, "Gorunfile")),
        (2, code(CHECKS, CFG[0] + 3, CFG[0] + 4, "Gorunfile")),
        (2, code(CHECKS, CFG[0] + 4, CFG[0] + 5, "Gorunfile")),
    ]),
    ("checkrun", None, [
        (3, run_output("gorun Gorunfile --run --plain", "c-run1")),
        (3, run_output("gorun Gorunfile --run --plain", "c-run2")),
        (3, table_slide("What the check's exit code means", [
            ("exits 0", "nothing to do: the task is unchanged, and skipped"),
            ("anything else", "something to do: the task runs"),
            ("can't finish", "the task fails, rather than run blind"),
            ("no check", "the task always runs"),
        ])),
    ]),
    ("dryrun", None, [
        (3, run_output("gorun Gorunfile --run --plain --check", "c-dry")),
        (3, run_output("gorun Gorunfile --run --plain --force", "c-force")),
    ]),
    ("verify", None, [
        (3, code(VERIFY, VCFG[0] + 3, VCFG[0] + 4, "Gorunfile")),
        (4, run_output("gorun Gorunfile --run --plain", "v-fail")),
    ]),
    ("copies", None, [(1, code(DEMO, SITE[0], SITE[1], "Gorunfile"))]),
    ("reliable", None, [
        (3, code(DEMO, UPDATE[0], UPDATE[1], "Gorunfile")),
        (3, code(DEMO, SMOKE[0], SMOKE[1], "Gorunfile")),
    ]),
    ("secrets", None, [
        (3, code(SECRETS, SEC_TASK[0], SEC_TASK[1], "Gorunfile")),
        (3, shell([("cmd", "cat .env")] + [("out", ln) for ln in text_file("secrets-env")], ".env  (kept out of git)")),
        (4, run_output("gorun Gorunfile --run --plain -v", "secrets-run")),
        (4, slide("How a secret stays secret", [
            ("", "The Gorunfile holds only a name", "${REGISTRY_TOKEN}"),
            ("", "The value comes from your environment or .env", "never from the file"),
            ("", "It reaches the task over stdin", "not in logs, history, events or the process list"),
        ])),
    ]),
    ("envvars", None, [
        (3, code(DEMO, GLOBAL[0], GLOBAL[0] + 1, "Gorunfile")),
        (3, code(DEMO, WEB4[0], WEB4[1], "Gorunfile")),
        (3, slide("Which one wins", [
            ("", "1. the task", "the most specific"),
            ("", "2. the server", ""),
            ("", "3. the server's groups", ""),
            ("", "4. the whole file", "the default"),
        ])),
    ]),
    ("templates", None, [
        (4, table_slide("Templates: what a server's settings can read", [
            ("{{ .Name }}", "the server's name"),
            ("{{ .Hostname }}", "its address"),
            ("{{ index .EnvVars \"APP_ENV\" }}", "one of its environment variables"),
            ("{{ .Server.Vars.port }}", "one of its server variables"),
            ("{{ .vars.workers }}", "one of the task's own variables"),
        ])),
        (4, code(TEMPLATES, T_HELLO[0], T_HELLO[1], "Gorunfile", font=28)),
        (4, code(TEMPLATES, T_SERVERS[0], T_STAGING[1], "Gorunfile")),
        (4, run_output("gorun Gorunfile --run --plain -v", "templates-run", skip=3, take=12)),
    ]),
    ("render", None, [
        (4, code(TEMPLATES, T_CONFIG[0], T_CONFIG[1] + 1, "Gorunfile")),
        (3, shell([("cmd", "cat app.conf.tmpl")] + [("out", ln) for ln in TMPL_FILE.rstrip().split("\n")], "app.conf.tmpl")),
        (4, run_output("gorun Gorunfile --run --plain -v", "templates-run", skip=12, take=28)),
    ]),
    ("fleet1", None, [
        (3, code(DEMO, NGINX[0], NGINX[1], "Gorunfile")),
        (3, code(DEMO, ANNOUNCE[0], ANNOUNCE[1], "Gorunfile")),
        (3, tmux("f-check-fresh")),
        (3, tmux("f-run-mid")),
        (2, tmux("f-run-done")),
    ]),
    ("fleet2", None, [
        (3, tmux("f-rerun-mid")),
        (3, tmux("f-rerun-done")),
        (3, tmux("f-change-mid")),
        (3, tmux("f-change-done")),
        (3, tmux("f-check-done")),
    ]),
    ("outro", None, [(1, slide("Next time", [
        ("", "Splitting a big Gorunfile across files", "include"),
        ("", "Sharing tasks between projects", "from a git repository"),
        ("", "Running on a schedule", "cron patterns and one-off times"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "problems": "What goes wrong",
    "checkintro": "Checks: skip work that's done",
    "dryrun": "Dry runs and forcing",
    "verify": "Verify: prove it worked",
    "copies": "Checking copies",
    "reliable": "Timeouts, retries and rerun safety",
    "secrets": "Secrets",
    "envvars": "Environment variables",
    "templates": "Templates",
    "render": "Template files and raw",
    "fleet1": "Putting it together",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "03-reliable-deployments")
