#!/usr/bin/env python3
"""Builds video 10, "The built-in assistant": real screenshots of gorun -serve's Assistant tab and
Generate form, answered by the built-in model, plus slides and a Chatterbox voiceover -> MP4,
captions and chapters.

    PW=/path/with/playwright-core ./record.sh     # captures the web UI (needs Go, unshare, chromium, node)
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
from common import page, shell, slide  # noqa: E402
from narration import NARRATION  # noqa: E402

common.init(HERE)
CAP = common.CAP
BADGE = "Demo run: stand-in servers, real model answers"


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">$ gorun -serve Gorunfile</div>'
        "<h1>The built-in <span class=acc>assistant</span></h1>"
        '<div class="sub">Explain failures, summarise runs and write tasks, on your own machine.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 10 of the series</div></div>'
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


def first_use():
    # what -serve prints on the first question, with the defaults a reader would see
    lines = [
        "gorun is serving on http://localhost:8080 (listening on :8080, all interfaces)",
        "Press ctrl+c to stop.",
        "downloading llama-b12359-bin-ubuntu-vulkan-x64.tar.gz  36 MB",
        "downloading Qwen3-4B-Instruct-2507-Q4_K_M.gguf  2.5 GB",
        "  kept in ~/.cache/gorun/llm",
    ]
    return shell([("cmd", "gorun -serve Gorunfile")] + [("out", ln) for ln in lines], "bash")


SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("start", None, [
        (5, shot("assistant", "The Assistant tab, in gorun -serve")),
        (9, first_use()),
        (7, slide("Private by default", [
            ("", "The model runs on this machine", "your Gorunfile and run history don't leave it"),
            ("", "It reads", "your tasks, bundles and servers, and the recent runs"),
            ("warn", "It never sees", "env vars or template variables"),
        ])),
    ]),
    ("diagnose", None, [
        (4, shot("live-done", "nginx failed on web-3")),
        (4, shot("run-page", "History: Ask the assistant")),
        (3, shot("diagnose-thinking", "Why did this run fail?")),
        (8, shot("diagnose", "Port 80 is already in use on web-3")),
    ]),
    ("summary", None, [
        (8, shot("summary", "Summarise the recent runs")),
        (6, shot("servers", "Which tasks run on which servers?")),
    ]),
    ("write", None, [
        (8, shot("write", "A task with a check and a verify, checked to load")),
        (5, shot("write-added", "Added as unsaved changes")),
    ]),
    ("generate", None, [
        (6, shot("generate-form", "Config: Generate tasks")),
        (6, shot("generate-done", "Review, edit what's wrong, then save")),
    ]),
    ("models", None, [(1, slide("Other models", [
        ("", "Another model file", "GORUN_LLM_MODEL=/path/to/model.gguf (or an https link)"),
        ("", "A model server", "GORUN_LLM_URL=http://localhost:11434/v1 (Ollama, LM Studio, hosted)"),
        ("warn", "Not on your machine?", "the page warns you, in amber"),
    ]))]),
    ("outro", None, [(1, slide("Recap", [
        ("", "Explain and summarise", "failures, runs and your Gorunfile"),
        ("", "Write tasks", "checked to load, saved only when you say"),
        ("", "Private", "runs on your machine, never runs anything itself"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "start": "Starting the assistant",
    "diagnose": "Why did it fail?",
    "summary": "Summaries and questions",
    "write": "Writing tasks",
    "generate": "Generate tasks",
    "models": "Other models",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "10-assistant")
