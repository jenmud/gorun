VERDICT: CHANGES

Human rework (this overrides the spec's "out of scope" for a second ctrl-c):
- A second ctrl-c while waiting for shutdown must quit immediately, without waiting.
- The first abort line must tell the user so, e.g. "abort: received interrupt, waiting for running tasks to stop (press ctrl+c again to force quit)".
- Add tests for both, and document it in docs/running.md.
