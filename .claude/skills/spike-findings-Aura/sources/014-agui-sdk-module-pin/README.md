---
spike: 014
name: agui-sdk-module-pin
type: standard
validates: "Given the monorepo subdir module github.com/ag-ui-protocol/ag-ui/sdks/community/go, when go get @e9e910b runs in Aura's module under Go 1.26, then it resolves (pseudo-version), go build ./... stays green, and the amendment-#6 CI grep format is confirmed or disproven"
verdict: VALIDATED
related: [015-agui-event-surface, 016-agui-sse-roundtrip]
tags: [agui, go-sdk, go-mod, phase-12]
---

# Spike 014: AG-UI Go SDK — module pin & build

## What This Validates

Given the monorepo subdir module `github.com/ag-ui-protocol/ag-ui/sdks/community/go`, when `go get @e9e910b230b9329c905e31ca024b4114dedf7918` (2026-05-14, the exact "interrupt, resume, multimodal" commit amendment #6 demands ≥) runs in Aura's module under Go 1.26.4, then the module resolves, `go build ./... && go vet ./...` stay green, and the amendment-#6 CI grep gate format is testable against the real go.mod line.

## Research

- Repo tags: 100+ `release/YYYY-MM-DD` tags, **zero** `sdks/community/go/vX.Y.Z` subdir tags → Go can only ever record a **pseudo-version** for this module. Confirmed live: `go get @<full-sha>` resolves to `v0.0.0-20260514093510-e9e910b230b9`.
- SDK `go.mod`: `go 1.24.4` (Aura's 1.26.4 satisfies), deps `google/uuid v1.6.0` (identical to Aura's — zero conflict), `sirupsen/logrus v1.9.3`, `testify v1.7.0` (test-only, not linked).
- Last commit touching the SDK as of 2026-06-06 **is** `e9e910b` itself — HEAD pin and amendment-#6 pin coincide today.

## How to Run

```bash
# from repo root (dependency must be present in go.mod):
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events@v0.0.0-20260514093510-e9e910b230b9  # transitive sums
go run -tags spike_agui ./.planning/spikes/014-agui-sdk-module-pin
```

The session reverted go.mod/go.sum after spiking (the dependency lands for real with Phase 12); the harness is build-tagged `spike_agui` so the committed tree builds clean without it.

## What to Expect

Forensic log: `[MODULE]` resolved pseudo-version assertion, `[EVENT]` wire-shape JSON for TEXT_MESSAGE_CONTENT + RUN_STARTED, `[SUMMARY] VALIDATED`, exit 0.

## Investigation Trail

1. `gh api` recon: module path + go.mod confirmed at `sdks/community/go`; packages `client/core/encoding/errors`; no subdir version tags → pseudo-version is the *only possible* resolution. Predicted the amendment-#6 grep gate (`[a-f0-9]{40}` in go.mod) cannot match before running anything.
2. `go get @<40-char-sha>` in Aura's module: resolved + downloaded cleanly via proxy. Landed as `// indirect` (nothing imports it yet — becomes direct when `internal/agui` lands).
3. CI gate probe: `grep -E '^require github\.com/ag-ui-protocol/ag-ui/sdks/community/go [a-f0-9]{40}$' go.mod` → **0 matches, structurally unsatisfiable**: `require <path> <40-hex>` is not valid go.mod syntax; Go records `v0.0.0-20260514093510-e9e910b230b9` (12-char SHA embedded in a pseudo-version).
4. First harness run failed: `missing go.sum entry for module providing package github.com/sirupsen/logrus` — `go get <module>` alone does NOT pull transitive sums for a module nothing imports. One extra `go get <module>/pkg/core/events@<pseudo-version>` fixes it. **Phase-12 gotcha**: the real `internal/agui` import makes this moot (`go mod tidy` handles it), but any CI bootstrap that pins before code imports it will hit the same error.
5. Re-run: VALIDATED, exit 0. `go build ./...` + `go vet ./...` green with and without the tag.

## Results

**VALIDATED ✓** — the pin works, the gate as written does not.

- Resolution: `go get @e9e910b...` → `v0.0.0-20260514093510-e9e910b230b9`, immutable via go.sum (6 new lines), runtime-asserted via `debug.ReadBuildInfo`.
- Footprint in Aura's module: +2 require lines (SDK + logrus indirect), uuid shared at v1.6.0, zero version conflicts, build+vet green.
- Wire shape first contact: `{"type":"TEXT_MESSAGE_CONTENT","timestamp":1780766295396,"messageId":"msg-1","delta":"Ciao"}` — camelCase per spec; SDK auto-adds optional `timestamp` (ms epoch).
- **Amendment needed (PRD #6 CI gate)**: replace the 40-hex grep with a pseudo-version pin check, e.g. `grep -E '^\s*github\.com/ag-ui-protocol/ag-ui/sdks/community/go v0\.0\.0-20260514093510-e9e910b230b9( // indirect)?$' go.mod`. The *intent* of #6 (no floating `latest`, no install-time resolution) is fully met by a pseudo-version: it names exactly one commit and go.sum seals it.
- **Surprise**: the SDK's `core/events` itself imports `logrus` (in `decoder.go`) — a logging dep inside a protocol library. Tolerable (~indirect, small), but it links into the Aura binary once `internal/agui` imports events. Worth a note in the Phase-12 plan; upstream may drop it later.
