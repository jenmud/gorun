VERDICT: PASS

n/a: no UI changes. The diff touches only plain output (cmd/plain.go, cmd/main.go), tests and docs. No TUI view and no .templ file changed.

- [nit] cmd/plain.go:37 abortLine is 100 characters, so it wraps on an 80-column terminal. Acceptable for plain output.
