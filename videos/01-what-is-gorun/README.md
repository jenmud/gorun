# Video 1: What is gorun?

An 8-minute introduction: what gorun is, why it exists, what it is not good for, how to install it,
the sections of a Gorunfile, and a demo that sets up five new Ubuntu servers with Docker and Caddy.
It is published at <https://go-run.dev/videos>.

The narration is a synthetic voice ([Chatterbox](https://github.com/resemble-ai/chatterbox), a local
model, imitating `voice-ref.wav`), and the video says so. The screens are real: slides and code are
rendered from `demo/Gorunfile`, and the picker and live view are captured from the gorun build in
`GORUN_SRC`.

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; synthesises the voice, renders frames, writes `build/01-what-is-gorun.mp4`, `captions.srt` and `chapters.json` |
| `record.sh` | captures the gorun screens (picker, dry run, live run, rerun) in tmux |
| `demo/` | the Gorunfile built on screen: five Ubuntu servers, Docker, and Caddy serving a stats page |
| `record/` | the same tasks and checks on local "servers" that only sleep, so the live view can be recorded without five real hosts |
| `voice-ref.wav` | a deep-voice sample (made with Kokoro) that Chatterbox imitates |

## Rebuild

Needs ffmpeg, chromium, tmux, Go, a CUDA GPU and the `chatterbox-tts` Python package (Python 3.11).

```bash
GORUN_SRC=/path/to/latest/gorun ./record.sh      # capture the screens with the release being shown
GORUN=gorun python build.py                      # voice, frames, video; output goes to build/ (gitignored)
```

`ENGINE=kokoro KOKORO=/path/to/kokoro-v1.0.onnx` uses a lighter voice instead; `TEMPO` (default 1.15)
sets the speaking speed. Scene audio is cached by a hash of its narration, so only scenes whose text
changed are re-voiced.

## Publish

The website serves the video itself. Encode the web files, copy them with the captions and the
chapters into `web/static/media/` in the [gorun-website](https://github.com/jenmud/gorun-website)
repo (see its `media/README.md`), and update `web/templates/videos.templ` if the chapters changed.
