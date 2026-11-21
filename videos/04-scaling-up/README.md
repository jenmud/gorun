# Video 4: Scaling up: include files, shared tasks and schedules

A 5-minute tutorial: splitting a Gorunfile with `include`, sharing tasks from a git repository
(`git::…?ref=`, `Gorunfile.lock`, `--update-includes`, and why a library can only add tasks), and
`[[schedule]]` (cron, one-off `at`, time zones, `missed`, the Schedules page). Same synthetic voice,
pace and look as videos [1](../01-what-is-gorun/), [2](../02-gorunfile-and-tui/) and [3](../03-reliable-deployments/).

| File | What |
|---|---|
| `narration.py` | the spoken script, one entry per scene |
| `build.py` | scenes and slides; the shared rendering, voice and assembly code is in `../common.py` |
| `record.sh` | captures the output with the release in `GORUN_SRC` |
| `steps/split/` | the split fleet Gorunfile (SSH servers; listed, never contacted) |
| `steps/dup/`, `steps/schedbad/` | the real errors for a duplicate name and a bad cron pattern |
| `steps/library/`, `steps/gitinc/`, `steps/gitbad/` | a task library that `record.sh` turns into a git repository on this machine, and Gorunfiles that include it (a `file://` URL; yours would be https or ssh) |
| `steps/schedules/` | schedules that load in `gorun -serve`; the Schedules page is screenshotted with headless Chromium |

Everything in the video is captured from real runs; `record.sh` fails nothing silently, and uses a made-up
host and user name and the UTC time zone.

## Rebuild

Needs ffmpeg, chromium, Go, git, unshare, a CUDA GPU and the `chatterbox-tts` Python package (3.11).

```bash
GORUN_SRC=/path/to/latest/gorun ./record.sh
python build.py                                  # output goes to build/ (gitignored)
```
