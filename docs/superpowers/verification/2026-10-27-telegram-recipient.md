# Telegram recipient verification — 2026-10-27

Issue: https://github.com/chetto1983/Aura/issues/196

Base: `2e41ead35eaf48a7670af10df428c468f7723e30`.
Environment: isolated local Linux/arm64 containers, Go 1.27.3, sqlc 1.31.1,
PostgreSQL 18.6. Tests use the disposable `aura_cov` database, with all migrations
applied and synthetic accounts. No production credentials or Telegram API calls.

## Before / after

`TestIdentityDeliveryRequiresUnambiguousAccount` was run before changing the query
or Store. The two- and three-link cases failed: the original lookup returned the
first account with no error. Zero and one link, and the post-repair case, passed.

After the fix, the real Store and generated SQL reject both multiple-link cases
with `ErrAmbiguousAccount`. Text, conversation-bound and approval delivery each
record zero sends. One link sends to the expected synthetic recipient; zero links
retain the not-my-user result. An unrelated identity's account does not interfere.
Removing only the synthetic excess links restores the one-recipient behavior.

## Local results

| Check | Result |
| --- | --- |
| `go vet ./...` | Pass |
| `go build ./...` | Pass |
| `go test -tags db_integration ./internal/channels/telegram -count=1 -coverprofile=...` | Pass; 88.8% package statement coverage |
| `GetAccountByIdentity` / `singleRecipientAccount` coverage | 100% / 100% |
| `go test -race -tags db_integration ./internal/channels/telegram ./internal/channels -count=1` | Pass; includes package goleak checks |
| `go test ./internal/cron -count=1` | Pass |
| golangci-lint v2.14.0 on `./internal/channels/telegram`, default and `db_integration` tags | Pass; zero issues |
| `go vet -tags db_integration ./internal/channels/telegram` | Pass |
| sqlc regeneration | Pass; identical hashes after another `sqlc generate` |
| `bash scripts/check-file-size.sh` | Pass; new handwritten files are also below 600 lines |
| `git diff --check` | Pass |

Five targeted mutations in a separate temporary copy were all killed by assertion
failures (not compile failures): selecting the first ambiguous account, hiding
absence as success, losing the single recipient, truncating the generated query
to one row, and classifying ambiguity as not-found. This is a **5/5 sampled spot
check**, not an exhaustive mutation score for the package.

## Limits and review gates

`bash scripts/coverage_docker.sh` was attempted but stopped before its suite:
the temporary Go container has no Docker CLI (`docker: command not found`).
The full owned-surface coverage gate and `make quality-full` are not established.
The package's 88.8% is not a claim about repository-wide coverage. Full service
integration, repository-wide lint/race/vulnerability gates and live patch
acceptance remain for CI and maintainer validation before merge.

The earlier production repair was independently confirmed on the operator's
device, but that appliance still runs its original build. This PR does not deploy
code, delete existing mappings, enforce single-account enrollment, or replay old
failed notifications. A deployment with multiple links will now require explicit
operator reconciliation before identity-routed Telegram pushes can resume.
