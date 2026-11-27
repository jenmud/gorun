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
        '<div class="sub" style="margin-top:30px;color:var(--warn)">Experimental: a first version that needs much more training.</div>'
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
        "downloading gorun-qwen3-4b-v3-q4_k_m.gguf  2.5 GB",
        "  kept in ~/.cache/gorun/llm",
    ]
    return shell([("cmd", "gorun -serve Gorunfile")] + [("out", ln) for ln in lines], "bash")


SCENES = [
    ("intro", None, [(1, title_slide())]),
    ("start", None, [
        (5, shot("assistant", "The Assistant tab, in gorun -serve")),
        (9, first_use()),
        (6, shot("assistant-stats", "The Model panel: where it runs, and how fast")),
        (7, slide("Private by default", [
            ("", "The model runs on this machine", "your Gorunfile and run history don't leave it"),
            ("", "It reads", "your tasks, bundles and servers, and the recent runs"),
            ("warn", "It never sees", "env vars or template variables"),
        ])),
    ]),
    ("diagnose", None, [
        (4, shot("live-done", "Live: Ask the assistant")),
        (4, shot("run-page", "History: Ask the assistant")),
        (3, shot("diagnose-thinking", "Why did this run fail?")),
        (8, shot("diagnose", "Port 80 is already in use on web-3")),
    ]),
    ("summary", None, [
        (8, shot("summary", "Summarise the recent runs")),
        (6, shot("servers", "Which tasks run on which servers?")),
    ]),
    ("write", None, [
        (12, shot("write", "Small tasks, each with a check and a verify")),
        (12, shot("complex", "A bigger job: small tasks, and a schedule")),
        (5, shot("complex-saved", "Save these tasks: saved straight away")),
        (6, shot("tasks-saved", "Ready on the Tasks page")),
    ]),
    ("schedules", None, [
        (8, shot("sched", "Move nightly-disk to 4:30am, Sydney time")),
        (6, shot("sched-saved", "Saved: it replaces the schedule in place")),
        (10, shot("schedules", "gorun warns: the model forgot local = true")),
    ]),
    ("generate", None, [
        (6, shot("generate-form", "Config: Generate tasks")),
        (6, shot("generate-done", "Review, edit what's wrong, then save")),
    ]),
    ("experimental", None, [(1, slide("Experimental", [
        ("", "Writes working tasks about 8 times in 10", "on held-out tests; the stock model: about 5 in 10"),
        ("", "Sets up schedules 7 times in 8", "new ones, and changes to the ones you have"),
        ("warn", "Weak on general questions about gorun", "it can make up options: check go-run.dev"),
        ("warn", "Needs much more training", "review every task before you run it"),
    ]))]),
    ("models", None, [
        (8, shot("models-panel", "Models: open, close and switch")),
        (6, shot("models-search", "Search Hugging Face for GGUF models")),
        (7, shot("models-files", "Pick a size, and download it: checked before it opens")),
        (8, slide("Other models", [
        ("", "Another model file", "GORUN_LLM_MODEL=/path/to/model.gguf (or an https link)"),
        ("", "A model server", "GORUN_LLM_URL=http://localhost:11434/v1 (Ollama, LM Studio, hosted)"),
        ("warn", "Not on your machine?", "the page warns you, in amber"),
        ("", "One model for everyone", "questions wait in a queue: no extra memory"),
    ])),
    ]),
    ("training", None, [
        (6, shot("training-chat", "With capture on, rate each answer")),
        (8, shot("training-page", "Training data: what was kept, and how it went")),
        (6, slide("Help train it", [
            ("", "Off until an admin turns it on", "and the page tells everyone it's on"),
            ("", "Kept in your history database", "the prompt, the reply, saved or not, how it ran"),
            ("", "Export JSONL", "addresses and keys replaced; nothing is sent anywhere"),
        ])),
    ]),
    ("outro", None, [(1, slide("Recap", [
        ("", "Explain and summarise", "failures, runs and your Gorunfile"),
        ("", "Write small tasks", "each with a check and a verify, chained with depends_on"),
        ("", "Set up schedules", "new ones, or changes to the ones you have"),
        ("warn", "Experimental", "private, never runs anything itself: review every task"),
    ]))]),
]

CHAPTERS = {
    "intro": "Welcome",
    "start": "Starting the assistant",
    "diagnose": "Why did it fail?",
    "summary": "Summaries and questions",
    "write": "Writing tasks",
    "schedules": "Schedules",
    "generate": "Generate tasks",
    "experimental": "How good is it?",
    "models": "Other models",
    "training": "Help train it",
    "outro": "Wrap up",
}

if __name__ == "__main__":
    common.build(SCENES, NARRATION, CHAPTERS, "10-assistant")
