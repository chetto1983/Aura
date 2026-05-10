---
phase: 01-fondamenta-concurrency-safety
plan: "01"
subsystem: qdrant
tags: [qdrant, client, interface, http, vector-search]
dependency_graph:
  requires: []
  provides: [internal/qdrant.Client, internal/qdrant.NewClient, internal/qdrant.WaitForReady]
  affects: [internal/search, internal/tools]
tech_stack:
  added: [internal/qdrant]
  patterns: [interface-driven-client, exponential-backoff, httptest-mock-server]
key_files:
  created:
    - internal/qdrant/types.go
    - internal/qdrant/config.go
    - internal/qdrant/client.go
    - internal/qdrant/client_test.go
  modified: []
decisions:
  - "NewClient returns (Client, error) to surface empty-BaseURL failures at construction time (T-01-04)"
  - "CollectionInfo returns zero value on 404 instead of error (Pitfall 3: warm-cache first-startup)"
  - "WaitForReady timeout error includes endpoint URL and elapsed time for operator diagnostics (D-20)"
  - "doRequest caps response at 4MB via io.LimitReader to prevent DoS from large Qdrant responses (T-01-02)"
  - "API key transmitted in api-key header only, never in URL query params (T-01-03)"
metrics:
  duration: "5 minutes"
  completed_date: "2026-05-10T12:43:14Z"
  tasks_completed: 3
  tasks_total: 3
  files_created: 4
  files_modified: 0
---

# Phase 01 Plan 01: internal/qdrant shared client package Summary

**One-liner:** Shared Qdrant REST client interface with httpClient impl, WaitForReady startup gate, and 10 mock-server tests covering health, batching, warm-cache 404 handling, and exponential backoff.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Create types.go and config.go | fb0a5bc | internal/qdrant/types.go, internal/qdrant/config.go |
| 2 | Create client.go with Client interface, httpClient, NewClient, WaitForReady | 9a280c3 | internal/qdrant/client.go |
| 3 | Create client_test.go with 10 mock-server tests | 6bafcec | internal/qdrant/client_test.go |

## What Was Built

The `internal/qdrant` package provides a single, canonical Qdrant REST client to replace two duplicate implementations (`qdrantClient` in `internal/search/qdrant.go` and `toolVectorIndex` in `internal/tools/registry_search_vector.go`). Downstream plans 01-03 and 01-04 will migrate callers to use this shared interface.

### Key components

**types.go** -- `Point`, `ScoredPoint`, `CollectionInfo` structs with JSON tags exactly matching Qdrant's REST API wire format.

**config.go** -- `Config` struct and `DefaultConfig()` with 30s HTTP timeout and 10s max retry delay defaults.

**client.go** -- `Client` interface (7 methods) + `httpClient` concrete implementation:
- `Health` -- GET /readyz, returns nil on 2xx
- `Search` -- POST /collections/{name}/points/query with vector and limit
- `Upsert` -- PUT /collections/{name}/points?wait=true in chunks of 64
- `Delete` -- POST /collections/{name}/points/delete?wait=true in chunks of 64; 404 = success
- `CreateCollection` -- PUT /collections/{name}; 200 and 409 = success
- `DeleteCollection` -- DELETE /collections/{name}; 404 = success
- `CollectionInfo` -- GET /collections/{name}; 404 returns zero `CollectionInfo` (not error)
- `WaitForReady` -- package-level function: exponential backoff 500ms..10s, timeout error includes endpoint and elapsed time
- `doRequest` helper -- JSON marshal, Content-Type, authorize, 4MB response limit via `io.LimitReader`

**client_test.go** -- 10 tests using `net/http/httptest.NewServer`:
1. TestHealth_OK
2. TestHealth_Error
3. TestWaitForReady_Immediate
4. TestWaitForReady_Timeout (completes in ~500ms, well under 2s)
5. TestWaitForReady_EventualSuccess (atomic counter: first 2 calls fail, then succeed)
6. TestCollectionInfo_Success
7. TestCollectionInfo_NotFound (Pitfall 3 verification)
8. TestNewClient_EmptyBaseURL
9. TestUpsert (100 points -> 2 batches assertion)
10. TestSearch

## Verification

```
go build ./internal/qdrant/   -- PASS
go vet ./internal/qdrant/     -- PASS
go test -count=1 ./internal/qdrant/  -- PASS (10/10 tests)
go build ./...                -- PASS
```

Note: `go test -race` requires CGO (gcc), which is not available in this build environment. All 10 tests pass without the race detector. The `WaitForReady` and concurrent test helpers use `atomic.Int32` and channel-based mutex to be race-safe by construction.

## Deviations from Plan

### None for Tasks 1 and 2

Plan executed exactly as specified.

### Task 3: Race detector not available (environment limitation)

**Found during:** Task 3 verification  
**Issue:** `go test -race` requires CGO enabled, but `gcc.exe` is not present at the configured path (`D:\tmp\w64devkit\bin\gcc.exe`). This is an environment limitation, not a code defect.  
**Fix:** Tests run without `-race`. Concurrency safety is achieved structurally: `WaitForReady_EventualSuccess` uses `atomic.Int32` for the call counter; `TestUpsert` uses a buffered channel as a mutex for the captured bodies slice.  
**Impact:** Race detector coverage deferred; will be available in Docker CI environment where gcc is present.

## Known Stubs

None -- all methods are fully implemented with real HTTP logic.

## Threat Surface Scan

No new network endpoints or auth paths introduced beyond what is declared in the plan's threat model. The `internal/qdrant` package is a client library only (no server-side surfaces). All STRIDE mitigations from the plan's threat register are implemented:

| Threat ID | Status |
|-----------|--------|
| T-01-01 | Mitigated: WaitForReady timeout + exponential backoff |
| T-01-02 | Mitigated: 4MB io.LimitReader in doRequest |
| T-01-03 | Mitigated: api-key header only, no URL embedding |
| T-01-04 | Mitigated: NewClient returns error on empty BaseURL |
| T-01-05 | Accepted: same-network assumption, no TLS enforcement |

## Self-Check: PASSED

- [x] internal/qdrant/types.go exists
- [x] internal/qdrant/config.go exists
- [x] internal/qdrant/client.go exists
- [x] internal/qdrant/client_test.go exists
- [x] Commit fb0a5bc exists (Task 1)
- [x] Commit 9a280c3 exists (Task 2)
- [x] Commit 6bafcec exists (Task 3)
- [x] go build ./... passes
- [x] All 10 tests pass
