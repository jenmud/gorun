# Video 9: Copying files

A 3-minute tutorial: `copy` tables (a file, a folder, inline content, `render = true` templates, `mode`), the
rules for copies, upload progress in the live view and in `--plain` logs, and `check_copies` to send only what
changed, with `--check` to see what would. Same synthetic voice, pace and look as videos
[1](../01-what-is-gorun/), [2](../02-gorunfile-and-tui/), [3](../03-reliable-deployments/),
[4](../04-scaling-up/), [5](../05-web-dashboard/), [6](../06-go-library/), [7](../07-otel-and-nats/) and
[8](../08-ci-and-automation/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | starts three SSH servers from the recipes lab, makes a release, runs gorun on `demo/` over SSH and captures the output and the live view (in tmux) |
| `demo/` | the Gorunfile, a template and a small site |

The servers are real: three containers from `recipes/docker-compose.yml` (`ssh-server-1` to `-3`), started
by `record.sh` and stopped afterwards. The copies, the progress and the checks are real output. The release
is random data, `SIZE_MB` megabytes (600 by default).

## Rebuild

Needs ffmpeg, chromium, Go, Docker, tmux, unshare, a CUDA GPU and the `chatterbox-tts` Python package
(3.11). The lab images (`lscr.io/linuxserver/openssh-server`) are pulled by Docker if missing.

```bash
./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
