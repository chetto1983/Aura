# Phase07B Benchmark - Typed Collection Registry

Status: **closed 2026-05-16** — all G.1 checks passed via US-L01..L05 (commits
1a6a609a, 92e446fb, ca6a86e3, bb0ed864, 508b32a1).

## Audit §G.1 Acceptance Criteria — Actuals

| G.1 Item | Acceptance Criterion | Files / Lines | Status |
| --- | --- | --- | --- |
| G.1.1 Typed Collection Registry | `type Collection string` + 6 constants declared | `internal/storage/memoryindex/collections.go` — `Collection`, `CollectionWiki…CollectionOperational` | **met** (US-L01 1a6a609a) |
| G.1.1 Typed Collection Registry | `CollectionDescriptor` struct + `Registry` map + `Lookup()` | `internal/storage/memoryindex/collections.go` — `CollectionDescriptor`, `Registry`, `Lookup` | **met** (US-L01 1a6a609a) |
| G.1.1 Back-compat alias | `KindSource/Archive/Proposal` derived from `string(Collection*)` | `internal/storage/memoryindex/store.go:16-20` | **met** (US-L02 92e446fb) |
| G.1.2 Score components — compact index | `Document.ScoreExact/FTS/Vector` preserved through `mergeDocumentsRRF` | `internal/storage/memoryindex/store.go` — `Document` struct + `mergeDocumentsRRF` | **met** (US-L03 ca6a86e3) |
| G.1.2 Score components — wiki search | `Result.ScoreExact/FTS/Vector` preserved through `mergeHybridResults` | `internal/storage/search/search.go` — `Result` struct + `mergeHybridResults` | **met** (US-L03 ca6a86e3) |
| G.1.3 Follow-up handles | `formatMemoryResults` emits `[exact=… fts=… vector=…]` + `follow_up=` per hit | `internal/agent/tools/registry/memory_search.go:formatMemoryResults` | **met** (US-L04 bb0ed864) |
| G.1.3 Follow-up handles | Only existing tool names used in `follow_up=` mapping | Verified by grep `internal/agent/tools/registry/` — `read_source`, `read_memory`, `search_memory` all registered | **met** (US-L04 bb0ed864) |
| G.1.4 Chunk-to-parent expansion | `Filter.SourceID` field + `filterWhere` predicate exposed | `internal/storage/memoryindex/store.go` — `Filter.SourceID`, `filterWhere` | **met** (US-L05 508b32a1) |
| G.1.4 Chunk-to-parent expansion | `memory_search` tool accepts `source_id` param + forwards to `Filter.SourceID` | `internal/agent/tools/registry/memory_search.go` — `source_id` parameter schema entry | **met** (US-L05 508b32a1) |

## Required Commands

| Check | Command | Expected | Actual | Status |
| --- | --- | --- | --- | --- |
| Full build | `go build ./...` | zero output | zero output | **met** (verified after each US-L story) |
| Vet | `go vet ./...` | zero output | zero output | **met** (verified after each US-L story) |
| Memoryindex tests | `go test ./internal/storage/memoryindex/... -count=1` | green | PASS — collections, back-compat, score-components, SourceID tests all pass | **met** |
| Search tests | `go test ./internal/storage/search/... -count=1` | green | PASS — mergeHybridResults score-component test passes | **met** |
| Memory search tool tests | `go test ./internal/agent/tools/registry/... -count=1` | green | PASS — follow_up + bracket emission tests pass | **met** |
| Full test suite | `go test ./... -count=1` | green | PASS — all packages green after US-L05 | **met** |

## Deferrals (not in G.1 — not evaluated here)

G.2.1 (freshness registry), G.2.2 (user/operational memory), G.2.3 (span offsets),
G.2.4 (frontmatter promotion) — deferred to Phase 7C/7D per audit §G.2. Not
measured in this benchmark.
