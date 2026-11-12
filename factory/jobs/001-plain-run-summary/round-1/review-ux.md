VERDICT: PASS

- [nit] The line reads "1 servers, 1 tasks" for singular counts. The spec fixes one shape for easy grepping, so keep it, but a one-line note in `docs/running.md` that the shape never changes would help script authors.
- [nit] `docs/running.md` does not say what `ok` counts (tasks that ran and succeeded) versus `unchanged` (check found nothing to do). The old line explained "their checks found nothing to do"; that hint is now gone.
- [nit] `docs/reference.md` and `docs/integrations.md` were not checked for plain-output descriptions by the builder; grep them for the old unchanged line wording if present.
- Good: the `summary:` prefix is greppable, the example in the docs matches the real format, `--check` keeps its own line and the docs say so, and no new flags or keys were added.
