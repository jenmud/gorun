VERDICT: CHANGES

- [must] cmd/websrv/startup.go:84 (and :82), fed by cmd/websrv/nats.go:21,23: the NATS line prints
  `natsState.url`, which is `nc.ConnectedUrl()`, i.e. `url.URL.String()` of `GORUN_NATS_CLIENT_URL`
  with userinfo intact. `GORUN_NATS_CLIENT_URL=nats://user:pass@host:4222` now prints the password
  to stdout on every `-serve` start, and from there to terminal scrollback, journald or container
  logs. The same value also reaches the web header and Config page through `natsStatus()`. That
  exposure already existed, but this change adds stdout. Fix: in `natsStartState` set `url` from
  credential-free text. Drop the userinfo entirely (parse, set `u.User = nil`, `u.String()`), so a
  token given as `nats://TOKEN@host` is hidden too. `ConnectedUrlRedacted()` hides only the
  password. Add a test case to `TestStartupMessageNATS` (or a `natsStartState` test) that has
  credentials in the URL and asserts that neither the user/token nor the password appears.
- [nit] cmd/websrv/startup.go:86: the `off (<reason>)` text is `connectErr.Error()`. Current
  nats.go errors don't echo the URL, but it is safer to pass that text through the same scrubber.
- [nit] internal/natsserver/natsserver.go:56 (unchanged): logs `GORUN_NATS_CLIENT_URL` unredacted
  to the log file. This is out of scope here, but the same helper would fix it.

Checked and fine: no new shell/SSH/copy/template-to-shell paths. No new handlers, and no
mutation on GET. The bind error goes to stderr and contains only the address. go.mod and vendor are
unchanged, so there is no new dependency (`config_templ.go` was regenerated with the go.mod templ
version). The config load error and DB error come from file paths and parse errors, not from
secrets (secrets stay in env).
