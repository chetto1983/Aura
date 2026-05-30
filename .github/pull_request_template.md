<!-- Keep PRs focused. .planning/ artifacts are not part of code review. -->

## What & why

<!-- What does this change do, and why? Link the phase/slice or issue. -->

Closes #

## Type

- [ ] feat — new capability
- [ ] fix — bug fix
- [ ] refactor / perf
- [ ] docs
- [ ] test
- [ ] chore / ci / build

## Checklist

- [ ] `make quality` passes locally (vet · build · file-size · lint · race · vuln)
- [ ] `make quality-full` passes (coverage ≥ 85% on the owned surface) — or N/A
- [ ] Tests added/updated; no `t.Skip` used to hide a failure
- [ ] No file exceeds 600 LOC; touched files refactored on touch
- [ ] Conforms to `prd.md` (or includes a PRD-amendment commit), per `CLAUDE.md`
- [ ] Docs / `CHANGELOG.md` updated if behaviour or interfaces changed
- [ ] No secrets, credentials, or `.env` values committed

## Notes for reviewers

<!-- Anything non-obvious: trade-offs, follow-ups, areas wanting extra scrutiny. -->
