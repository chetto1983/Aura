# Contributing to Aura

Thanks for your interest. Aura is a self-hosted agent runtime built **PRD-first**:
the [`prd.md`](prd.md) is the source of truth and [`CLAUDE.md`](CLAUDE.md) is the
working contract. Read both before a non-trivial change — code that contradicts
the PRD won't be merged without a PRD-amendment first.

## Prerequisites

- **Go** — the version in [`go.mod`](go.mod) (currently 1.26.x).
- **Docker** — for the Postgres + ArcadeDB + embedding-sidecar stack (`compose.yaml`).
- A POSIX shell. **WSL** is the recommended dev environment on Windows — it runs
  the entire gate natively (gcc + make + CGO + the container stack via
  `127.0.0.1`). See `CLAUDE.md` §Quality tooling & gates for the cross-env matrix.

## One-time setup

```bash
make tools         # installs golangci-lint, govulncheck, dupl, lefthook, ...
lefthook install   # wires the pre-commit / pre-push git hooks
cp .env.example .env && $EDITOR .env   # set POSTGRES_PASSWORD / ARCADEDB_PASSWORD
```

## The quality gate

Every change must pass the same gate CI enforces:

```bash
make quality              # vet + file-size + lint(+dupl) + deadcode + test-race + vuln + go build  (no containers)
make db-migrate memory-up # bring the stack up (Postgres migrated, ArcadeDB + MCP + embed healthy)
make quality-full         # quality + coverage (owned surface >= 85%)
```

The lefthook hooks run a fast subset automatically: **gofmt/vet/lint/file-size/
jscpd on commit**, **quality-snapshot-freshness/build/deadcode/web gates on
push**. (`golangci-lint` runs at commit, not push, so a lint regression surfaces
at the commit that introduced it.) Note the pre-push **quality-snapshot
freshness gate** — if a file you changed matches a CI-gate glob in
`docs/aura-quality-snapshot.md`, that row's `Last measured` date must be
re-attested or the push is rejected. Don't `--no-verify` to dodge a real
failure — fix it.

Discipline that reviewers check (full list in `CLAUDE.md`):

- **No file > 600 LOC.** Refactor on touch.
- **Coverage floor 85%** on the owned surface, across the full tag matrix.
- **No skip-as-green.** Integration tests must actually run; they `t.Fatal` under
  `$CI` when their env is unset.
- **Tests are real:** race detector, goleak, property-based where indicated,
  realistic fixtures. No `t.Skip` to paper over a failure.
- **Mutation spot-check ≥ 70%** on each phase's critical file(s).

## Commits & PRs

- **Conventional Commits**: `type(scope): subject` — the scope is the phase or
  plan, e.g. `feat(37F-04): …`, `test(42): …`, `docs(infra): …`. Imperative
  subject; body explains *why*.
- Add the project trailer: `Co-Authored-By: …` where applicable.
- Keep PRs focused. Planning artifacts under `.planning/` are not part of code
  review — use `gsd-pr-branch` to produce a clean PR branch when needed.
- Fill in the PR template checklist. Green CI (build/lint/race/integration/
  CodeQL/vuln/coverage) is required to merge.

## Reporting security issues

**Do not open a public issue.** Follow [`SECURITY.md`](SECURITY.md).
