VERDICT: PASS

- [nit] docs/running.md: the Ctrl+C text is appended to the `--check` sentence, so it reads as part of that note. Start it as its own short paragraph ("Press Ctrl+C to stop a plain run. ...").
- [nit] docs/running.md: say that a force quit leaves that run unfinished in history (build.md notes this). Users who read the history view will otherwise wonder.
- [nit] The abort line is about 105 characters and wraps on 80-column terminals. Acceptable, but "abort: interrupted, waiting for tasks to stop (ctrl+c again to force quit)" fits.
- [nit] A second ctrl+c pressed in the instant between the first one and the watcher registering for signals is lost. A fast double-tap may need a third press. Rare, no change needed.
- Good: the first line says how to force quit, the force-quit line says what happens, exit code 130 is documented, and no new flags or keys.
