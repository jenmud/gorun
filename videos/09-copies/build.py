#!/usr/bin/env python3
"""Builds video 9, "Copying files": real copies to three SSH servers (Docker), progress, check_copies, slides
and a Chatterbox voiceover -> MP4, captions and chapters.

    ./record.sh                                   # captures the runs (needs Go, Docker, tmux, unshare)
    ./build.py                                    # voice needs a CUDA GPU; see ../common.py

Output goes to ./build/ (gitignored). Shared code is in ../common.py.
"""

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
from common import code, page, shell, slide, text_file, tmux  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
GORUNFILE = (HERE / "demo" / "Gorunfile").read_text()


def out(cmd, name, keep=None, drop=None):
    lines = []
    for ln in text_file(name):
        if keep and not keep(ln):
            continue
        if drop and drop(ln):
            continue
        lines += [("out", w) for w in (textwrap.wrap(ln, 78, subsequent_indent="      ", break_long_words=False, replace_whitespace=False) or [""])]
    return shell([("cmd", cmd)] + lines)


def site_run():
    """The site run's lines that matter: web-1's config check, its site task with its output, the summary."""
    lines = text_file("site")
    picked = [ln for ln in lines if ln.startswith("web-1: config unchanged")]
    i = next(n for n, ln in enumerate(lines) if ln.startswith("web-1: site ok"))
    picked.append(lines[i])
    for ln in lines[i + 1:]:
        if not ln.startswith("    "):
            break
        picked.append(ln)
    picked += [ln for ln in lines if ln.startswith("summary:")]
    return shell([("cmd", "gorun Gorunfile -r --plain -v --task site"), ("out", "…")] + [("out", ln) for ln in picked])


def block(start):
    lines = GORUNFILE.split("\n")
    i = next(n for n, ln in enumerate(lines) if ln.startswith(start))
    j = i + 1
    while j < len(lines) and lines[j].strip() != "":
        j += 1
    return i, j


def toml(start, title="Gorunfile"):
    a, b = block(start)
    return code(GORUNFILE, a, b, title, window_lines=26, font=30)


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">[[tasks.release.copy]]</div>'
        "<h1>Copying <span class=acc>files</span></h1>"
        '<div class="sub">Templates, folders, progress and check_copies.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 9 of the series</div></div>'
    )


def gofile(text, title):
    n = len(text.rstrip("\n").split("\n"))
    return code(text, 0, n, title, lang="go", window_lines=12, font=32)


PROGRESS_GO = """cfg.OnEvent = func(event any) {
    if ev, ok := event.(gorun.CopyProgressEvent); ok {
        fmt.Printf("%s: %s %.0f%% (%d of %d bytes)\\n",
            ev.Server, ev.File, ev.Percent, ev.Bytes, ev.Total)
    }
}
"""

SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("basics", None, [
        (7, toml("[tasks.config]")),
        (7, out("gorun Gorunfile -r --plain -v --task config", "config")),
    ]),
    ("rules", None, [(1, slide("Rules for copies", [
        ("", "Copies run as the login user", "no sudo: copy to /tmp, then move it into place"),
        ("", "They overwrite, and create folders", "mode applies to every copied file"),
        ("", "owner and group need sudo = true", "they are set with chown afterwards"),
        ("", "Files are streamed", "and stopping the run stops the copy"),
    ]))]),
    ("folders", None, [
        (4, toml("[tasks.site]")),
        (6, site_run()),
    ]),
    ("progress", None, [
        (4, toml("[tasks.release]")),
        (5, tmux("tui-3", title="gorun", minw=1700, font=21, lh=1.3)),
        (5, tmux("tui-6", title="gorun", minw=1700, font=21, lh=1.3)),
        (7, out("gorun Gorunfile -r --plain --task release | grep '^web-1'", "release", keep=lambda t: t.startswith("web-1"))),
    ]),
    ("checks", None, [
        (5, out("gorun Gorunfile -r --plain", "rerun")),
        (6, out("gorun Gorunfile -r --plain -v --check | grep -E 'would run|changed'", "check-after-edit", keep=lambda t: "would run" in t or "changed" in t)),
        (5, out("gorun Gorunfile -r --plain | grep -E 'site|release|summary'", "run-after-edit", keep=lambda t: ("site ok" in t) or ("release" in t and "->" not in t) or t.startswith("summary"))),
    ]),
    ("library", None, [(1, gofile(PROGRESS_GO, "your program"))]),
    ("outro", None, [(1, slide("Recap", [
        ("", "[[tasks.x.copy]]", "a file, a folder, or inline content; render = true for templates"),
        ("", "Progress", "a bar, the bytes and the rate, in every view"),
        ("", "check_copies = true", "send only what changed"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "basics": "Copy tables",
    "rules": "Rules",
    "folders": "Folders",
    "progress": "Upload progress",
    "checks": "check_copies",
    "library": "In the Go library",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "09-copies")
