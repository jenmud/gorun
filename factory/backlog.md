# Backlog

`/factory next` runs the first unchecked item and ticks it. Add one feature per line.

- [x] Print a one-line summary at the end of a `-plain` run: servers, tasks run, skipped, failed, and total time. (job 001-plain-run-summary)
- [ ] Add a `timeout` to a whole server group, so every task on a group's servers gets it unless the task sets its own.
- [ ] Let the web dashboard filter the run view by task status (running, failed, skipped).
- [ ] Stop logging NATS credentials: reuse cmd/websrv withoutUserinfo in internal/nats/client.go (log, OTEL metric and span attributes) and internal/natsserver/natsserver.go; show a fixed message for a NATS URL with a path (token containing /).
