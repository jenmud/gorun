---
name: factory
description: Run gorun's software factory. A feature goes through spec, build, four parallel reviews and approval, then merges to master. Use for /factory <feature>, /factory next, /factory approve <job-id> and /factory rework <job-id> <note>.
---

# The software factory

You are the **orchestrator**. You run the loop below, spawn every agent and are the only one who
writes `factory/board.json`. You don't write code, specs or reviews yourself. The agents do that,
and each one writes only its own file.

## The loop (edit this to change the factory)

```
feature → spec-writer (opus) → builder (opus)
        → [reviewer-security (opus), reviewer-ux (sonnet), reviewer-ui (sonnet), reviewer-code (sonnet)]
        → WAIT FOR ALL 4 reviews (any CHANGES: resume the builder, at most MAX_FIX_ROUNDS times)
        → all PASS → approver (opus)
        → APPROVE: merge to master  /  ESCALATE: needs-human ◄── /factory approve
```

- `MAX_FIX_ROUNDS = 2`. The builder can be sent back twice, so a job has at most 3 review rounds.
- `REVIEWERS = security, ux, ui, code`. Agent `reviewer-<r>` writes `round-<n>/review-<r>.md`.
- The default branch is `master`. Never push. The human pushes.
- Run one job at a time.

## Files

```
factory/board.json                 # you write it, the dashboard reads it
factory/backlog.md                 # "- [ ] feature" lines for /factory next
factory/jobs/<id>/feature.md       # you: the request, as the human wrote it
factory/jobs/<id>/spec.md          # spec-writer
factory/jobs/<id>/build.md         # builder: what changed, how it was tested (rewritten each round)
factory/jobs/<id>/round-<n>/review-<r>.md   # reviewer-<r>, starts "VERDICT: PASS" or "VERDICT: CHANGES"
factory/jobs/<id>/round-<n>/review-human.md # you, for /factory rework only
factory/jobs/<id>/decision.md      # approver, starts "APPROVE" or "ESCALATE"
```

A job id is `NNN-slug`: the next free 3-digit number in `factory/jobs/`, then a short kebab-case
slug of the feature (`003-rate-limiting`). Its branch is `factory/<id>`.

## board.json

Rewrite the whole file each time something changes, so the dashboard moves as the job does.

```json
{ "jobs": [ {
  "id": "003-rate-limiting", "title": "Rate limit the web API",
  "branch": "factory/003-rate-limiting",
  "stage": "review", "round": 2,
  "reviews": { "security": "CHANGES", "ux": "PASS", "ui": "pending", "code": "PASS" },
  "rounds": [ { "security": "CHANGES", "ux": "PASS", "ui": "PASS", "code": "CHANGES" } ],
  "decision": null, "note": "", "updated": "2026-11-12T10:04:00Z"
} ] }
```

- `stage` is one of `spec`, `build`, `review`, `approve`, `needs-human`, `merged`.
- `reviews` is the current round: `pending`, `PASS` or `CHANGES` per reviewer.
- `rounds` holds the finished earlier rounds, oldest first.
- `note` says why a job needs a human, in one line.

## /factory <feature>

1. Pick the job id. Create `factory/jobs/<id>/` and write the feature text to `feature.md`.
   Add the job to the board with stage `spec`.
2. **Spec.** Spawn `spec-writer` and wait for it: "Job <id>. Read factory/jobs/<id>/feature.md,
   write factory/jobs/<id>/spec.md."
3. **Build.** Set stage `build`, round 1. Spawn `builder` **with the name `builder-<id>`** and wait
   for it: "Job <id>. Read factory/jobs/<id>/spec.md. Work on branch factory/<id>, write
   factory/jobs/<id>/build.md."
4. **Review round n.** Set stage `review`, and set every reviewer to `pending`. In one message, spawn
   the four reviewers in the background: "Job <id>, round <n>. Read factory/jobs/<id>/spec.md and
   build.md, review `git diff master...factory/<id>`, write
   factory/jobs/<id>/round-<n>/review-<r>.md." As each one finishes, read the first line of its
   file and update its chip on the board.
5. **Wait for all.** The round is done only when all four `review-*.md` files exist in
   `round-<n>/` (check with `ls`). Never resume the builder before that.
6. **Fix or approve.**
   - Any `VERDICT: CHANGES`, and fewer than `MAX_FIX_ROUNDS` fix rounds used: move `reviews` into
     `rounds`, set stage `build`, round n+1, then resume the same builder with SendMessage
     (to `builder-<id>`): "Round <n> reviews are in factory/jobs/<id>/round-<n>/. Fix every
     CHANGES item on factory/<id>, rewrite build.md." When it replies, go to step 4 with round n+1.
   - Any CHANGES, with the fix rounds used up: set stage `needs-human` and note
     "still CHANGES after round <n>". Stop.
   - All PASS: go on.
7. **Approve.** Set stage `approve`. Spawn `approver` and wait for it: "Job <id>. Read
   factory/jobs/<id>/, write factory/jobs/<id>/decision.md."
   - `APPROVE`: merge (below), stage `merged`, decision `APPROVE`.
   - `ESCALATE`: stage `needs-human`, decision `ESCALATE`, note = the first reason in decision.md.
8. Tell the human in a few lines: the job id, its stage, and what to read next.

If an agent fails or doesn't write its file, set stage `needs-human` with a note, and stop.

## Merge

```bash
git switch master
git merge --no-ff factory/<id> -m "Merge factory/<id>: <title>"
make test
git add factory/jobs/<id> && git commit -m "factory: record job <id>"
```

If the merge conflicts or `make test` fails, run `git merge --abort` (or `git reset --hard
ORIG_HEAD` after a merge that went through), set stage `needs-human`, and note why.

## /factory next

Take the first `- [ ]` line in `factory/backlog.md`. Mark it `- [x] … (job <id>)`, then run it as
`/factory <feature>`. If nothing is unchecked, say so.

## /factory approve <job-id>

The human approves a `needs-human` job. Do the merge above, then set stage `merged`, decision
`APPROVE (human)`. Refuse a job that isn't `needs-human`.

## /factory rework <job-id> <note>

The human sends a job back with feedback.

1. Write `factory/jobs/<id>/round-<n>/review-human.md` in the job's latest round folder: the first
   line is `VERDICT: CHANGES`, then the note.
2. Move `reviews` into `rounds`, set stage `build`, round n+1. Rework gives the job a fresh
   `MAX_FIX_ROUNDS`.
3. Resume `builder-<id>` with SendMessage, pointing it at `round-<n>/`. If that builder is gone (a new
   session), spawn a new `builder` named `builder-<id>` with the same pointer. It picks up from
   build.md and the branch.
4. Carry on from step 4 of the loop.

## Notes

- SendMessage may be a deferred tool. Load it with ToolSearch (`select:SendMessage`) before you
  need it.
- The dashboard is `factory/dashboard.html`. To view it, run
  `cd factory && python3 -m http.server 8000` and open http://localhost:8000/dashboard.html.
