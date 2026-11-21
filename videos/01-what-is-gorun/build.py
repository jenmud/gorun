#!/usr/bin/env python3
"""Builds the "What is gorun?" video: slides + terminal frames + a Kokoro voiceover -> MP4 and SRT.

    ./record.sh                      # capture the gorun screens (needs tmux, Go)
    ./build.py                                    # Chatterbox voice (needs a GPU)
    ENGINE=kokoro KOKORO=/path/to/kokoro-v1.0.onnx ./build.py   # lighter fallback

Needs ffmpeg, chromium, and the kokoro-onnx and soundfile Python packages. Output goes to ./build/ (gitignored).
"""

import hashlib
import json
import html
import os
import re
import subprocess
import sys
import wave
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent.parent / "docs" / "screenshots"))
from ansi2html import convert  # noqa: E402
from narration import NARRATION  # noqa: E402

OUT = HERE / "build"
CAP = OUT / "cap"
KOKORO = os.environ.get("KOKORO", "kokoro-v1.0.onnx")  # model; voices-v1.0.bin sits next to it
VOICE = os.environ.get("VOICE", "am_michael")
TEMPO = float(os.environ.get("TEMPO", "1.15"))  # playback speed of the synthesised voice
TEMPO_KEY = "deep-v1"  # bump to force a re-synthesis when the voice settings change
ENGINE = os.environ.get("ENGINE", "chatterbox")  # chatterbox (GPU, default) or kokoro
GORUN = os.environ.get("GORUN", "gorun")
W, H = 1920, 1080

# --- visual building blocks -------------------------------------------------------------------

CSS = """
:root{--bg:#0f1117;--card:#181b24;--line:#2a2f3d;--fg:#e8eaf0;--dim:#8b93a7;--acc:#5eead4;--acc2:#7dd3fc;--warn:#fbbf24;--bad:#f87171}
*{box-sizing:border-box}
html,body{margin:0;width:1920px;height:1080px;background:var(--bg);color:var(--fg);overflow:hidden;
  font-family:"Noto Sans","Inter",sans-serif}
.mono{font-family:"JetBrainsMono Nerd Font","JetBrains Mono",monospace}
.wrap{padding:90px 120px;height:100%}
h1{font-size:96px;margin:0 0 24px;letter-spacing:-1px}
h2{font-size:64px;margin:0 0 40px;letter-spacing:-.5px}
.acc{color:var(--acc)} .dim{color:var(--dim)}
.sub{font-size:40px;color:var(--dim);line-height:1.4}
ul{list-style:none;padding:0;margin:0}
li{font-size:44px;line-height:1.35;margin:0 0 26px;padding-left:64px;position:relative}
li:before{content:"";position:absolute;left:8px;top:20px;width:22px;height:22px;border-radius:50%;background:var(--acc)}
li.bad:before{background:var(--bad)} li.warn:before{background:var(--warn)}
li small{display:block;font-size:30px;color:var(--dim)}
.brand{position:absolute;left:120px;bottom:60px;font-size:30px;color:var(--dim)}
.chrome{background:#0b0b0d;border:2px solid var(--line);border-radius:18px;overflow:hidden;
  box-shadow:0 30px 80px rgba(0,0,0,.5)}
.bar{display:flex;gap:12px;align-items:center;padding:16px 22px;background:#1b1e28;color:var(--dim);font-size:24px}
.bar i{width:18px;height:18px;border-radius:50%;display:block}
.pane{padding:26px 34px;white-space:pre;color:#d4d4d8;font-variant-ligatures:none}
.badge{position:absolute;right:60px;top:40px;font-size:26px;color:var(--warn);border:2px solid var(--warn);
  border-radius:999px;padding:6px 22px;background:#0f1117cc}
table{border-collapse:collapse;width:100%;font-size:36px}
th,td{padding:22px 28px;text-align:left;border-bottom:2px solid var(--line);vertical-align:top}
th{color:var(--dim);font-weight:600;font-size:30px}
"""


def page(body, extra=""):
    return f'<!doctype html><meta charset="utf-8"><style>{CSS}{extra}</style>{body}'


def slide(title, bullets=(), sub="", kind=""):
    items = "".join(
        f'<li class="{k}">{t}{f"<small>{s}</small>" if s else ""}</li>' for k, t, s in bullets
    )
    return page(
        f'<div class="wrap"><h2>{title}</h2>{f"<div class=sub>{sub}</div>" if sub else ""}<ul>{items}</ul></div>'
        '<div class="brand mono">gorun</div>'
    )


def title_slide():
    return page(
        '<div class="wrap" style="display:flex;flex-direction:column;justify-content:center">'
        '<div class="mono acc" style="font-size:40px;margin-bottom:20px">$ gorun Gorunfile --run</div>'
        "<h1>What is <span class=acc>gorun</span>?</h1>"
        '<div class="sub">Run plain shell commands on a pile of servers,<br>in parallel, and watch it happen.</div>'
        '<div class="sub" style="margin-top:70px;font-size:30px">Tutorial 1 of the series</div></div>'
    )


def window(title, inner, font=24, lh=1.3, badge="", minw=0):
    return page(
        '<div class="wrap" style="display:flex;align-items:center;justify-content:center;padding:40px">'
        f'<div class="chrome"><div class="bar"><i style="background:#f87171"></i><i style="background:#fbbf24"></i>'
        f'<i style="background:#4ade80"></i><span class="mono" style="margin-left:14px">{html.escape(title)}</span></div>'
        f'<div class="pane mono" style="font-size:{font}px;line-height:{lh};min-width:{minw}px">{inner}</div></div></div>'
        + (f'<div class="badge">{badge}</div>' if badge else "")
    )


def tmux(name, badge="", title="gorun"):
    text = (CAP / f"{name}.txt").read_text(errors="replace")
    # the live run is on local stand-ins; show the addresses the real servers would have
    text = "\n".join(
        re.sub(r"local\b", f"10.0.0.1{m.group(1)}", ln) if (m := re.search(r"node-(\d)", ln)) else ln
        for ln in text.split("\n")
    )
    text = "\n".join(ln.rstrip() for ln in text.rstrip("\n").split("\n"))
    return window(title, convert(text), font=26, lh=1.25, badge=badge, minw=1560)


def shell(lines, title="bash"):
    """lines: list of (kind, text) where kind is 'cmd' (typed) or 'out'."""
    out = []
    for kind, text in lines:
        t = html.escape(text)
        out.append(f'<span style="color:#5eead4">❯</span> <span style="color:#fafafa">{t}</span>' if kind == "cmd"
                   else f'<span style="color:#9aa3b5">{t}</span>')
    return window(title, "\n".join(out), font=32, lh=1.5)


TOML_SECTION = re.compile(r"^\[.*\]$")


def hl_toml(line):
    s = html.escape(line)
    if line.startswith("#"):
        return f'<span style="color:#6b7280">{s}</span>'
    if TOML_SECTION.match(line.strip()):
        return f'<span style="color:#7dd3fc;font-weight:bold">{s}</span>'
    m = re.match(r"^(\s*)([\w.-]+)( = )(.*)$", s)
    if m:
        return f'{m.group(1)}<span style="color:#c4b5fd">{m.group(2)}</span>{m.group(3)}<span style="color:#fcd34d">{m.group(4)}</span>'
    return s


def code(text, start, end, title, lang="toml", window_lines=17, font=33):
    """Show text lines up to `end`, with lines in [start, end) bright and the rest dimmed."""
    lines = text.rstrip("\n").split("\n")
    first = max(0, end - window_lines)
    rows = []
    for i in range(first, end):
        body = hl_toml(lines[i]) if lang == "toml" else html.escape(lines[i])
        op = "1" if i >= start else ".38"
        rows.append(f'<span style="opacity:{op}">{body or " "}</span>')
    return window(title, "\n".join(rows), font=font, lh=1.35)


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
         ("", "A single Go program", "runs on your laptop or CI"),
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

# --- pipeline ---------------------------------------------------------------------------------


def spoken(text):
    # respell for the voice: "gorun" -> "go run"; keep SSH/TOML letter-by-letter
    text = text.replace("Gorunfile", "Go run file").replace("gorun", "go run")
    return text.replace("TOML", "tomel").replace("SSH", "S S H").replace("SUDO_USER", "sudo user")


_tts = None


def speak(text, wav):
    """Synthesise sentence by sentence so pauses vary like a person reading, then join."""
    global _tts
    import numpy as np
    import soundfile as sf

    if _tts is None:
        if ENGINE == "chatterbox":
            from chatterbox.tts import ChatterboxTTS

            _tts = ChatterboxTTS.from_pretrained(device="cuda")
        else:
            from kokoro_onnx import Kokoro

            _tts = Kokoro(KOKORO, str(Path(KOKORO).with_name("voices-v1.0.bin")))

    def synth(sent):
        if ENGINE == "chatterbox":
            return _tts.generate(spoken(sent), audio_prompt_path=str(HERE / "voice-ref.wav"), exaggeration=0.3, cfg_weight=0.5).squeeze().cpu().numpy(), _tts.sr
        return _tts.create(spoken(sent), voice=VOICE, speed=0.98, lang="en-us")

    chunks, rate = [], 24000
    for pi, para in enumerate(text.split("\n")):
        for si, sent in enumerate(re.split(r"(?<=[.!?])\s+", para)):
            audio, rate = synth(sent)
            gap = 0.6 if (pi and not si) else 0.3
            if chunks:
                chunks.append(np.zeros(int(rate * gap), dtype=audio.dtype))
            chunks.append(audio)
    sf.write(str(wav), np.concatenate(chunks), rate)


def run(*cmd, **kw):
    subprocess.run(cmd, check=True, **kw)


def render(html_text, png):
    f = png.with_suffix(".html")
    f.write_text(html_text)
    run("chromium", "--headless", "--disable-gpu", "--hide-scrollbars", f"--window-size={W},{H}",
        f"--screenshot={png}", f"file://{f}", stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def duration(path):
    with wave.open(str(path)) as w:
        return w.getnframes() / w.getframerate()


def srt_time(t):
    ms = int(round(t * 1000))
    return f"{ms // 3600000:02}:{ms // 60000 % 60:02}:{ms // 1000 % 60:02},{ms % 1000:03}"


def main():
    for d in ("img", f"voice-{ENGINE}-deep"):
        (OUT / d).mkdir(parents=True, exist_ok=True)
    pad = 0.7
    video_list, audio_list, srt, t = [], [], [], 0.0
    chapters = []
    for n, (sid, _, shots) in enumerate(SCENES):
        wav = OUT / f"voice-{ENGINE}-deep" / f"{sid}-{hashlib.sha1((NARRATION[sid] + TEMPO_KEY).encode()).hexdigest()[:8]}.wav"
        text = NARRATION[sid]
        if not wav.exists():
            speak(text, wav)
        fast = wav.with_suffix(".fast.wav")  # speed up without changing pitch
        run("ffmpeg", "-y", "-loglevel", "error", "-i", str(wav), "-af", f"atempo={TEMPO}", str(fast))
        wav = fast
        total = duration(wav) + pad
        if sid in CHAPTERS:
            chapters.append({"t": round(t, 1), "title": CHAPTERS[sid]})
        audio_list.append(wav)
        wsum = sum(w for w, _ in shots)
        for k, (w, h) in enumerate(shots):
            png = OUT / "img" / f"{n:02}-{sid}-{k}.png"
            render(h, png)
            video_list.append((png, total * w / wsum))
        # captions: split into sentences, timed by character share of the speech
        sents = re.split(r"(?<=[.!?])\s+", text.replace("\n", " "))
        speech, cursor = duration(wav), t
        for s in sents:
            d = speech * len(s) / len(text)
            srt.append((cursor, cursor + d, s))
            cursor += d
        t += total
        print(f"{n:02} {sid:9} {total:6.1f}s")

    with open(OUT / "video.txt", "w") as f:
        for png, d in video_list:
            f.write(f"file '{png}'\nduration {d:.3f}\n")
        f.write(f"file '{video_list[-1][0]}'\n")
    with open(OUT / "audio.txt", "w") as f:
        for wav in audio_list:
            f.write(f"file '{wav}'\n")
    (OUT / "captions.srt").write_text("".join(
        f"{i}\n{srt_time(a)} --> {srt_time(b)}\n{s}\n\n" for i, (a, b, s) in enumerate(srt, 1)))

    # audio: concat with a pause after each scene, then normalise loudness
    padded = []
    for wav in audio_list:
        p = wav.with_suffix(".pad.wav")
        run("ffmpeg", "-y", "-loglevel", "error", "-i", str(wav), "-af", f"apad=pad_dur={pad}", str(p))
        padded.append(p)
    with open(OUT / "audio.txt", "w") as f:
        for p in padded:
            f.write(f"file '{p}'\n")
    run("ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", str(OUT / "audio.txt"),
        "-af", "loudnorm=I=-16:TP=-1.5:LRA=11", str(OUT / "voice.wav"))
    run("ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", str(OUT / "video.txt"),
        "-i", str(OUT / "voice.wav"), "-i", str(OUT / "captions.srt"),
        "-vf", "fps=30,format=yuv420p", "-c:v", "libx264", "-crf", "18", "-preset", "medium",
        "-c:a", "aac", "-b:a", "192k", "-c:s", "mov_text", "-metadata:s:s:0", "language=eng",
        "-movflags", "+faststart", "-shortest", str(OUT / "01-what-is-gorun.mp4"))
    (OUT / "chapters.json").write_text(json.dumps({"duration": round(t, 1), "chapters": chapters}, indent=1))
    print(f"done: {OUT / '01-what-is-gorun.mp4'} ({t / 60:.1f} min)")


if __name__ == "__main__":
    main()
