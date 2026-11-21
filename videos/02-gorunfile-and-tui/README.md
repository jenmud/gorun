# Video 2: Your first Gorunfile, and the terminal interface

An 8-minute step-by-step tutorial: the five concepts (task, server, group, tag, bundle), a Gorunfile
written in five steps, then the terminal interface: the picker, the live view, the history, and
running without the picker. Same synthetic voice, pace and look as [video 1](../01-what-is-gorun/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | captures the gorun screens in tmux with the release in `GORUN_SRC` |
| `steps/` | the first two Gorunfiles, really run on this machine (under a made-up host and user name) |
| `demo/` | the final Gorunfile built on screen: a web fleet over SSH (nothing is contacted); the picker is captured from it |
| `record/` | the same tasks on local "servers" that only sleep (web-3's smoke test fails on purpose), for the live view and history |

The live view and history frames are a stand-in run, labelled "Demo run, sped up" in the video.

## Rebuild

Needs ffmpeg, chromium, tmux, Go, unshare, a CUDA GPU and the `chatterbox-tts` Python package (3.11).

```bash
GORUN_SRC=/path/to/latest/gorun ./record.sh      # capture the screens with the release being shown
python build.py                                  # voice, frames, video; output goes to build/ (gitignored)
```

Scene audio is cached by a hash of its narration, so only scenes whose text changed are re-voiced.
`../01-what-is-gorun/README.md` explains publishing a video to the website.
