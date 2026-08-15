# Phase 45 — Deferred Items (out of this plan's scope, logged rather than fixed)

Per the executor's SCOPE BOUNDARY rule ("only auto-fix issues DIRECTLY caused by the
current task's changes"), items below were found during 45-08 Task 1's full gate run
but are NOT caused by Phase 45's code and are outside this plan's declared
`files_modified` (`docker/aura/Dockerfile`, `compose.yaml`,
`.planning/phases/45-harness-correctness/45-VALIDATION.md`,
`docs/aura-quality-snapshot.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`).

## 1. `make vuln` (govulncheck) red — 7 newly-disclosed stdlib CVEs, go1.26.5 → go1.26.6

**Found:** 2026-08-15, running `make quality` in WSL as part of 45-08 Task 1.

**Measured:** `deadcode`, `vet`, `file-size`, `embedding-model-contract`, `lint` (0 issues),
`test-race` (every package `ok`) all ran GREEN, in that order, before `vuln` failed —
confirmed by `make`'s sequential-prerequisite short-circuit (`quality: deadcode vet
file-size embedding-model-contract lint test-race vuln`, `Makefile:110`). Only `vuln` is
red.

`govulncheck ./...` reports 7 Go standard-library vulnerabilities, ALL against
`go1.26.5` (the pinned toolchain in `go.mod` and `docker/aura/Dockerfile:24`
`golang:1.26.5-alpine`), ALL fixed in `go1.26.6`:

| ID | Package | Fix |
|----|---------|-----|
| GO-2026-6218 | net/url | quadratic complexity in resolvePath |
| GO-2026-6091 | html/template | JS regexp context tracking |
| GO-2026-6090 | crypto/tls | post-handshake message limit |
| GO-2026-6089 | net/http | ReadHeaderTimeout on unencrypted HTTP/2 check |
| GO-2026-6088 | encoding/xml | recursion depth guard |
| GO-2026-5972 | encoding/asn1 | recursion depth guard |
| GO-2026-5026 | net/http (x/net/idna) | ASCII-only punycode label rejection |

None of the trace paths govulncheck reports touch anything Phase 45 changed
(idempotency key derivation, call-id dedup, memory supersede, MCP contract) — every
trace is through ordinary stdlib call sites (`http.Server.ListenAndServe`,
`tls.Conn.Handshake`, `xml.Decoder.Decode`, etc.) used throughout the whole tree.

**Confirmed pre-existing / environmental, not a Phase 45 regression:** the most recent
green CI run on `master` (`31720548572`, 2026-08-13T16:24:54Z, two days before this
measurement) shows `Supply-chain vulnerability scan (govulncheck)` as `success`. The
govulncheck vulnerability database is continuously updated independent of any code
change in this repo — these 7 CVEs were evidently disclosed and added to the DB
between 2026-08-13 and 2026-08-15. This is toolchain-version drift over time, not
something Phase 45's diff introduced or can fix without a toolchain bump.

**Why not auto-fixed:** bumping `go1.26.5` → `go1.26.6` touches `go.mod`'s
`go`/`toolchain` directive, `docker/aura/Dockerfile`'s `golang:1.26.5-alpine` base
(and likely `docker/aura-ingest/Dockerfile` and any other pinned Go base images), and
CI workflow `setup-go` version pins — none of which are in 45-08's declared
`files_modified`, and none of which are "directly caused by" this task's Dockerfile
VCS_REF stamp or compose.yaml build-args change. This is a repo-wide infrastructure
decision, not a narrow bug fix, so it is logged here rather than silently applied.

**Disposition:** left open, flagged to the human at the Task 1 → Task 2 boundary. The
plan's own Task 1 action text is explicit: "If any tier is red, STOP and report which
one with its output. Do not proceed to the live scenario on a red gate."
