VERDICT: PASS

The change prints two constant lines (`abortLine`, `forceQuitLine`) under the printer mutex and adds a
second-interrupt watcher on the plain path. No task, server, template or env data reaches the new
output, so no secrets can leak there. Nothing new reaches a shell, an SSH session, a copy path or a
web handler. No new dependencies; `os/signal` was already imported in `cmd/main.go`. `notifyInterrupts`
registers only `os.Interrupt`, only after the first abort, and `signal.Stop` runs via the deferred
`unregister` before `stop()` returns.

- [nit] cmd/main.go:323, a force quit (`os.Exit(130)`) skips deferred cleanup. The history DB is not
  closed and SSH sessions are dropped without waiting, so remote commands may keep running on the
  servers. The human asked for this and build.md and docs/running.md say so. No secret or
  integrity risk, but the docs could also say that remote commands may still be running after a
  force quit.
