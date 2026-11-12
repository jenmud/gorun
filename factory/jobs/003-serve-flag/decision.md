ESCALATE
The build meets AC1-AC12, every round-3 review passed, and `-serve` is additive (`-web` still works), so the CLI does not break.
It adds secret-handling code the spec did not ask for: `cmd/websrv/nats.go` strips NATS credentials (`withoutUserinfo`, the `scrubUserinfo` regex, a fixed reason for `*url.Error`) because the new startup message would print `GORUN_NATS_CLIENT_URL` to stdout.
Known gaps remain: a bare token that contains `/` can show its first part in the off reason, and `internal/nats/client.go` and `internal/natsserver/natsserver.go` still send the raw URL to the log file, OTEL metrics and spans.
`config_templ.go` was regenerated with templ v0.3.1070 (the go.mod version, not the installed v0.3.1001), so its diff is large but only generator churn.
Question: do you accept this credential redaction as written, and track the remaining raw-URL leaks (log, OTEL attributes, a bare token with `/`) as a separate follow-up job?
