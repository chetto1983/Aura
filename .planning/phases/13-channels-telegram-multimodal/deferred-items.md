# Phase 13 — Deferred Items (out-of-scope discoveries)

Logged per the executor SCOPE BOUNDARY rule: pre-existing lint findings in files
NOT modified by the current plan. Do NOT fix these in the plan that discovered
them — they belong to the owning plan / a hygiene sweep.

## Discovered during 13-04 (golangci-lint on internal/channels/telegram)

Owner: plan 13-03 (these files were authored there; 13-04 did not touch them).

| File | Line | Linter | Finding |
|------|------|--------|---------|
| internal/channels/telegram/tables.go | 137 | errcheck | `bodyFace.Close()` return value not checked (`defer bodyFace.Close()`) |
| internal/channels/telegram/tables.go | 142 | errcheck | `headFace.Close()` return value not checked (`defer headFace.Close()`) |
| internal/channels/telegram/mdv2.go | 81 | staticcheck QF1002 | could use tagged switch on `c` |
| internal/channels/telegram/mdv2_test.go | 126 | staticcheck QF1002 | could use tagged switch on `c` |
| internal/channels/telegram/mdv2_test.go | 142 | staticcheck QF1002 | could use tagged switch on `c` |

Note: these did not fail CI at 13-03 close (the project lint gate may run a
different ruleset / these may be advisory). Surfaced here so the phase-close
hygiene sweep or the renderer plan (13-05, which consumes tables.go/mdv2.go) can
fold the fixes on-touch. 13-04's own new files (registry.go, channel.go,
telegram/config.go, config.go additions) are golangci-lint-clean
(`--new-from-rev=HEAD` → 0 issues).
