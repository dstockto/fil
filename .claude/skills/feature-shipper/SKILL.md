---
name: feature-shipper
description: Implements a Ready roadmap item via strict red-green-refactor TDD and opens a draft PR. Use when the user has approved a roadmap-groomer proposal (says "go", "ship it", "do it") or directly names a Ready item to ship. Never auto-merges.
---

# feature-shipper

You ship a single roadmap item from `## Ready` in `roadmap.md` to a draft PR using **strict** red-green-refactor TDD. You do not merge. You do not retry indefinitely on failures. You stop short and report when the flow can't complete.

## Input

The slug of an item in `## Ready` of `roadmap.md`.

## Flow

### 1. Verify

Before touching anything:

```bash
# Working tree clean?
git status --short
# If any output: refuse.

# On main? If not, ask user to confirm "branch off current branch or off main".
# Default: off main.
git branch --show-current

# Slug in Ready?
# Read roadmap.md, confirm `### <slug>` lives under `## Ready`.

# Has Acceptance bullet?
# If `**Acceptance:**` is missing or the item has a `**Needs-spec:**`
# annotation, refuse: "This item is not Ready. Run roadmap-groomer to
# decide if it should be filed as needs-spec."
```

Bail conditions at this stage produce no side effects.

### 2. Branch

```bash
git checkout -b roadmap/<slug>
```

Do not touch `roadmap.md` yet. The item is still Ready as far as main is concerned.

### 3. Implement (strict RED-GREEN-REFACTOR)

For each unit of behavior the acceptance criteria call out:

#### RED

Write a failing test first. Use `_test.go`, table-driven where the project convention applies (see `cmd/find_test.go`, `cmd/plan_status_test.go` for patterns).

```bash
go test ./<package>/...
```

**Confirm the test fails.** If it passes, the test isn't actually testing the new behavior — fix the test before writing implementation.

#### GREEN

Write the minimum code that makes the failing test pass. Don't over-implement. Don't add features the test doesn't cover.

```bash
go test ./<package>/...
```

**Confirm the test passes.** If it still fails, you have a bug in the implementation OR the test is wrong. Diagnose before moving on.

#### REFACTOR

Improve the code with tests still passing. Look for: duplicated logic, awkward names, missing comments on non-obvious WHY (per project convention, comments explain WHY, not WHAT). Re-run tests after each refactor.

#### Repeat

Move to the next unit of behavior. Each unit gets its own RED-GREEN-REFACTOR cycle. Do not write multiple tests at once, do not implement ahead of tests.

### 4. Verify green

After all behavior is implemented:

```bash
go test ./...                  # full suite, not just changed package
golangci-lint run ./...
gofmt -w ./...
```

**Retry budget: 2 fix attempts per failure mode.** If `go test ./...` is red and you can't get it green in 2 attempts on the same failure, BAIL (see Bail section). Same for lint. Do not iterate forever — you'll burn quota grinding on something you don't understand.

### 5. Update roadmap.md (move Ready → In Flight)

Edit `roadmap.md` on this branch only. Move the `### <slug>` block from `## Ready` to `## In Flight`. Insert two metadata bullets **immediately after the `**Source:**` bullet** (so all metadata stays grouped at the top):

```
- **Acceptance:** ...        ← existing
- **Source:** ...             ← existing
- **Branch:** roadmap/<slug>  ← new
- **PR:** pending             ← new
```

(Leave `**Acceptance:**`, `**Source:**`, and any prose body in place.)

Commit:

```bash
git add roadmap.md <touched-source-files> <new-test-files>
git commit -m "roadmap: ship <slug>"
```

Match commit style: single-line subject, imperative, lowercase verb. Add a body if the change is non-obvious.

### 6. Push and open draft PR

```bash
git push -u origin roadmap/<slug>

gh pr create --draft \
  --base main \
  --title "roadmap: <slug>" \
  --body "$(cat <<'EOF'
## Summary
<1-3 bullets describing the change>

## Acceptance criteria
<verbatim from roadmap.md>

## Test plan
- [x] go test ./... green
- [x] golangci-lint run ./... clean
- [ ] <any manual verification you'd suggest>

Roadmap-Item: <slug>
EOF
)"
```

The `Roadmap-Item: <slug>` line is the parseable marker for `roadmap-merge-sync.yml`. It must be on its own line, exact format.

Capture the PR number from `gh pr create` output.

### 7. Amend roadmap.md with PR number

```bash
# Edit roadmap.md to replace `**PR:** pending` with `**PR:** #<N>`
git add roadmap.md
git commit -m "roadmap: record PR #<N> for <slug>"
git push
```

### 8. Report and stop

Print exactly what was done:

```
Draft PR #<N> opened: <url>
Branch: roadmap/<slug>
Tests: go test ./... green
Lint: golangci-lint clean
Status: ready for your review

Roadmap.md updated on this branch to move <slug> to In Flight.
On merge, roadmap-merge-sync.yml will move it to Done.
```

**Do not invoke `gh pr merge`. Do not push any further changes.** The PR sits as a draft for human review.

## Bail behaviors

Stop and report status; do not push, do not open PR, do not edit roadmap.md beyond what's been committed.

| Failure | Action |
|---|---|
| Dirty git tree at start | "Uncommitted changes on `<branch>` — stash or commit first, then re-invoke." |
| Slug not in `## Ready` | "Slug `<x>` not in Ready. Check `roadmap.md` or run roadmap-groomer." |
| Slug has `**Needs-spec:**` or no `**Acceptance:**` | "Item is not ship-ready. Run roadmap-groomer to decide next step." |
| Tests red after 2 fix attempts on the same failure | "Tests failing: `<test name>`. Tried 2 fixes, can't get green. Branch `roadmap/<slug>` is at HEAD; you can inspect and continue manually." |
| Lint red after 2 fix attempts | Same shape. |
| `gh pr create` fails | "PR creation failed: `<error>`. Roadmap.md edit rolled back. Branch still exists; you can retry manually." |
| User Ctrl-C mid-flow | Whatever's committed stays. No cleanup. User picks up manually. |

## Hard rules

- **Strict RED-GREEN-REFACTOR.** No "write the code then add tests." No "tests already exist" justifications. Each unit of new behavior gets a new failing test first.
- **Off main by default.** Branching off another branch requires user confirmation.
- **Draft PRs only.** Never `gh pr ready`, never `gh pr merge`.
- **One roadmap item per invocation.** If the user wants two items shipped, that's two separate invocations.
- **Do not chase scope creep.** If implementation reveals the acceptance criteria are wrong or incomplete, bail with a report rather than expanding the work.
- **No `--no-verify` on commits.** Pre-commit hooks (gofmt, golangci-lint, go-check) are load-bearing. If they fail, fix the cause.
- **No force-push.** If push fails due to remote race, pull-rebase once, retry, then bail.

## Existing codebase patterns to respect

- Tests: `_test.go` co-located, table-driven, no test framework dependencies (per `CLAUDE.md`).
- Comments: WHY, not WHAT. Only when non-obvious.
- Commit style: see `git log --oneline -10` — single-line imperative subjects, optional body for non-obvious changes.
- PR body: see `gh pr list --state all` if you want to match prior PR conventions. Failing that, the template above is fine.
