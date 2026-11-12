APPROVE
All four round-2 reviewers passed; AC1-AC7 and the human rework (second ctrl+c force quits, abort line says so) each have a test or doc change.
Scope matches the feature plus the human's amendment: cmd/plain.go, cmd/main.go, tests, docs/running.md only; no new flags, Gorunfile keys, library API or dependencies.
Signal handling is limited to os.Interrupt on the plain path, registered only after the first abort and unregistered before stop() returns; output is two constant lines, so no secret exposure.
Open nits (force quit leaves the history record unfinished and remote commands possibly running; docs placement; 100-char line) are inherent to the requested force quit or cosmetic. Worth a follow-up docs tweak, but none blocks the change.
