#!/usr/bin/env python3
"""Shared by the tutorial videos: slide and terminal rendering, the Chatterbox voiceover, and the
ffmpeg assembly of the MP4, captions and chapters. Each video's build.py supplies its scenes.

Needs ffmpeg, chromium, and (for the voice) chatterbox-tts on a CUDA GPU, or kokoro-onnx.
"""

import hashlib
import html
import json
import os
import re
import subprocess
import sys
import wave
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "docs" / "screenshots"))
from ansi2html import convert  # noqa: E402

OUT = CAP = None  # set by init()
KOKORO = os.environ.get("KOKORO", "kokoro-v1.0.onnx")  # model; voices-v1.0.bin sits next to it
VOICE = os.environ.get("VOICE", "am_michael")
TEMPO = float(os.environ.get("TEMPO", "1.15"))  # playback speed of the synthesised voice
TEMPO_KEY = "deep-v1"  # bump to force a re-synthesis when the voice settings change
ENGINE = os.environ.get("ENGINE", "chatterbox")  # chatterbox (GPU, default) or kokoro
VOICE_REF = Path(__file__).resolve().parent / "voice-ref.wav"  # the voice Chatterbox imitates
W, H = 1920, 1080


def init(video_dir):
    """Point the build at a video's directory: frames in build/cap, output in build/."""
    global OUT, CAP
    OUT = Path(video_dir) / "build"
    CAP = OUT / "cap"



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


ANSI = re.compile(r"\x1b\[[0-9;]*m")


def block(marker, text):
    """Line range [start, end) of the blank-line separated block that begins at `marker`."""
    lines = text.split("\n")
    i = next(n for n, ln in enumerate(lines) if ln.startswith(marker))
    j = i
    while j < len(lines) and lines[j].strip() != "":
        j += 1
    return i, j


def text_file(name):
    """The lines of a captured text file, without colour codes."""
    return ANSI.sub("", (CAP / f"{name}.txt").read_text()).rstrip("\n").split("\n")


def window(title, inner, font=24, lh=1.3, badge="", minw=0):
    return page(
        '<div class="wrap" style="display:flex;align-items:center;justify-content:center;padding:40px">'
        f'<div class="chrome"><div class="bar"><i style="background:#f87171"></i><i style="background:#fbbf24"></i>'
        f'<i style="background:#4ade80"></i><span class="mono" style="margin-left:14px">{html.escape(title)}</span></div>'
        f'<div class="pane mono" style="font-size:{font}px;line-height:{lh};min-width:{minw}px">{inner}</div></div></div>'
        + (f'<div class="badge">{badge}</div>' if badge else "")
    )


def tmux(name, badge="", title="gorun", relabel=None, minw=1560, font=26, lh=1.25):
    """A captured gorun screen in a window. relabel(text) may rewrite the text first."""
    text = (CAP / f"{name}.txt").read_text(errors="replace")
    if relabel:
        text = relabel(text)
    text = "\n".join(ln.rstrip() for ln in text.rstrip("\n").split("\n"))
    return window(title, convert(text), font=font, lh=lh, badge=badge, minw=minw)


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


# --- pipeline ---------------------------------------------------------------------------------


def spoken(text):
    # respell for the voice: "gorun" -> "go run"; keep SSH/TOML letter-by-letter
    text = text.replace("Gorunfile", "Go run file").replace("gorun", "go run")
    text = text.replace("TOML", "tomel").replace("SSH", "S S H").replace("SUDO_USER", "sudo user")
    return text.replace("TUI", "T U I").replace("CI", "C I").replace("MD5", "M D 5")


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
            return _tts.generate(spoken(sent), audio_prompt_path=str(VOICE_REF), exaggeration=0.3, cfg_weight=0.5).squeeze().cpu().numpy(), _tts.sr
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


def build(scenes, narration, chapter_titles, name):
    for d in ("img", f"voice-{ENGINE}-deep"):
        (OUT / d).mkdir(parents=True, exist_ok=True)
    pad = 0.7
    video_list, audio_list, srt, t = [], [], [], 0.0
    chapters = []
    for n, (sid, _, shots) in enumerate(scenes):
        wav = OUT / f"voice-{ENGINE}-deep" / f"{sid}-{hashlib.sha1((narration[sid] + TEMPO_KEY).encode()).hexdigest()[:8]}.wav"
        text = narration[sid]
        if not wav.exists():
            speak(text, wav)
        fast = wav.with_suffix(".fast.wav")  # speed up without changing pitch
        run("ffmpeg", "-y", "-loglevel", "error", "-i", str(wav), "-af", f"atempo={TEMPO}", str(fast))
        wav = fast
        total = duration(wav) + pad
        if sid in chapter_titles:
            chapters.append({"t": round(t, 1), "title": chapter_titles[sid]})
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
        "-movflags", "+faststart", "-shortest", str(OUT / f"{name}.mp4"))
    (OUT / "chapters.json").write_text(json.dumps({"duration": round(t, 1), "chapters": chapters}, indent=1))
    print(f"done: {OUT / (name + '.mp4')} ({t / 60:.1f} min)")
