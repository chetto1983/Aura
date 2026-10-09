---
name: steward
description: How to drive a pull request in the Aura repo to green and mergeable — conventions, how proactive to be, what to validate before every push, and how to handle Dependabot PRs and a red master. Use when opening, watching, fixing or babysitting an Aura PR, or when a CI, review or merge-conflict event arrives on one. For job-by-job CI diagnosis see the babysit skill.
---

# Steward: driving an Aura PR

Repo-specific rules for a PR you opened or were asked to drive. They add to CLAUDE.md;
where CLAUDE.md is stricter, CLAUDE.md wins. Job-level CI triage lives in `babysit`.

## How proactive to be

- **Red CI, a merge conflict or a red master is work now.** Do not wait for a reviewer.
- **Fix on touch, inside the PR's scope.** A defect in code the PR touches or breaks is fixed
  in this PR. A defect elsewhere goes to its own PR, or to the user with a proposed patch.
  Do not widen the PR silently.
- **Ask before** changing an API, schema, migration, env var, the PRD contract, or anything
  architectural. Say what you measured and what you propose. These need a PRD amendment
  first (CLAUDE.md, PRD-first principle).
- **Never** skip, disable or quarantine a test to get green. Never edit a test only to make
  it pass. A test changes only when it is wrong, and the commit body says why, as in
  `test(docs): pin the appliance docs contract where #129 moved it`.
- **3-strike rule.** The same failing approach three times means stop and report.

## Before every push

CI is slow: most jobs take 10 to 20 minutes and Stryker can run over an hour. One validated push beats three
speculative ones. Run, with Go 1.27.2 and `GOTOOLCHAIN=local` as CI does:

```bash
export PATH="$HOME/go/bin:$PATH"
go vet ./... && go build ./...
golangci-lint run <touched packages>     # must be v2.13.2 built with go1.27.x
go test -race -count=1 <touched packages>
go test -count=1 ./...                   # the whole unit suite, not just touched packages
```

- A golangci-lint built with an older Go refuses the module ("the Go language version ...
  is lower than the targeted Go version"). Rebuild it:
  `GOTOOLCHAIN=go1.27.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`.
- **Docs are code here.** `cmd/aura/distribution_artifacts_test.go` asserts phrases in
  `README.md`, `docs/INSTALL.md` and `docs/BACKUP-RESTORE.md`. After any docs move or
  rewrap, run `go test -count=1 ./cmd/aura/`. Grep for tests that read a file's content,
  not only for links to it. Missing this broke master in #129.
- **A docs-only PR gets no CI.** Changes limited to `docs/**/*.md` or `prd.md` trigger no
  workflow (the `paths` filter in `ci.yml`). Run the docs contract tests yourself.
- For a CI fix, reproduce the failure locally first, then show the same command passing.
- Re-read the diff adversarially before pushing. Files must stay at or under 600 LOC.

## Commits and branches

- Conventional Commits: `type(scope): imperative subject`, body explains why, measured
  numbers in the body. Dependency bumps use `build(deps)`, Actions bumps use `build(ci)`.
- One concern per commit. When a PR must carry an unrelated fix to get green (a red master,
  for example), it is a separate commit with its own body.
- PRs merge into `master` as merge commits. Bring master in with a merge, never a rebase or
  force-push on a branch someone else may have checked out.
- Lockfiles and generated code come from tooling, never by hand: `go mod tidy`,
  `sqlc generate` (v1.31.1), `npm ci` / `npm install` in `web/`.
- Migrations: `ls internal/db/migrations/ | tail -1` is the only source of the next number.

## Dependabot PRs

- **Never push to a `dependabot/*` branch.** Dependabot resolves conflicts on its PRs only
  "as long as you don't alter it yourself" (its PR body).
- A bump that breaks tests gets its own PR. Bump only the breaking module, fix the code or
  fixtures, and explain the upstream change with the file in the module that causes it.
  Dependabot then rebases or recreates the group without that module.
- Measure a bump without touching `go.mod`:
  `cp go.mod go.sum $S/ && GOFLAGS=-modfile=$S/go.mod go get <mod>@<ver> && GOFLAGS=-modfile=$S/go.mod go test ./...`.
- Check whether a vulnerable npm package ships. `dev` in `package-lock.json` means
  build-time only; say so when you rate the risk.

## Red master

If a check is red on `master` too, it is not this PR's, but it still blocks it. Find the
breaking commit. If a fix exists (its revert, or a fix PR), port it into this PR as its own
commit, and say so in one PR comment. If none exists, write the fix: a red master is the
highest-priority item in the repo.

## Done means

- CI green on the PR's current head and no merge conflict.
- Every review thread answered or resolved.
- The repo has no Claude Approvals check, so this is the whole bar.
- CodeQL `neutral` with "configuration not found" is a timing artifact (the summary ran
  before the PR's analysis uploaded). It is not a finding.
