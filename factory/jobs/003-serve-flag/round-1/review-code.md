VERDICT: CHANGES
gofmt clean (only the pre-existing examples/rollout and examples/servicedeck), go vet clean, go test -race ./... all ok.

- [must] recipes/05-scheduled-jobs/Gorunfile:2 still says "Schedules run inside `gorun -web`". The spec says examples and text use `gorun -serve`. AC9 only scans README/docs/Makefile, so the test misses it. Change it to `-serve`, and consider adding recipes/**/Gorunfile to the AC9 scan.
- [nit] recipes/07-big-upload/README.md: the `gorun -serve ...` line lost its comment alignment with the neighbouring lines (the `#` column is off by two).
- [nit] cmd/main.go: the `parseArgs` flag map line with "serve"/"web" is now over-long; fine, but splitting it would be tidier.
- [nit] cmd/websrv/web.go: if `loadConfig` fails after partial setup, `info.schedulerOn` is never read, so that case is fine. The extra "scheduler not started, see the log" branch in startup.go has no test (not required by the spec, but cheap to add).

Checked and fine: all 11 acceptance tests exist and assert what the spec asks. The bind-first flow and the `listen <addr>:` error go to stderr, and nothing is printed to stdout on failure. `a.mu` is held when reading cfgPath, cfg and sched. `config_templ.go` is regenerated from `config.templ`. No `.env`, db or binary is committed. No new TOML keys, so `Gorunfile.empty` needs no key change.
