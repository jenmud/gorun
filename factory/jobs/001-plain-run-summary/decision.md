APPROVE
All seven acceptance criteria are met and tested with exact-line assertions; all four round-1 reviews passed.
The change is limited to cmd/plain.go, a new cmd/plain_test.go and docs/running.md, which is what the feature asked for.
There are no new flags, Gorunfile keys, library API changes, dependencies or security-sensitive code.
The open nits ("1 servers" wording, a `break` that could read more clearly, docs not explaining ok vs unchanged) are cosmetic and do not add up to a real problem.
