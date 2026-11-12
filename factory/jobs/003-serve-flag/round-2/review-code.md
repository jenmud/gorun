VERDICT: PASS
gofmt clean (only the pre-existing examples/rollout and examples/servicedeck), go vet clean, go test -race ./... all ok.

- [nit] cmd/websrv/nats.go: `scrubUserinfo` is a regex over any `scheme://...@`; it can miss a userinfo containing `/` or whitespace in an error string. Acceptable for a best-effort scrub of the off reason.
- [nit] internal/nats/client.go still logs the full NATS URL on connect (to the log file only). Build.md already notes it as a follow-up; out of scope here.

Round-1 must fixed: recipes/05-scheduled-jobs/Gorunfile:2 now says `gorun -serve`, and the AC9 test scans recipes/**/Gorunfile. Round-1 nits fixed (07 README alignment, flag map split, scheduler-not-started test). The new `withoutUserinfo` is used for the embedded and external NATS URLs, so the startup message and both pages are clean. The bind-first flow, the `listen <addr>:` error on stderr and the exit in `Run` only are correct. No .env, db or binary committed.
