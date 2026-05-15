# Phase05 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Tool order snapshot | selected tool registry test | deterministic order | registry.Definitions() returns stable ordered slice; all registry tests green (commit ab843241) | **met** |
| Schema tests | selected tool schema tests | typed schema stable | definition_test.go covers ToolDefinition defaults + provider-supplied overrides for all new fields; examples_parameter_eval_test.go validates all Example.Arguments against Parameters JSONSchema (commit 28ae9324, 0 violations) | **met** |
| Probe harness | `cmd/probe_tools` or equivalent | behavior inspected | catalogue scan test (registry_scan_test.go) walks registry.Definitions() and fails loud on uncatalogued native tools; eval_topk_test.go exercises deferred-discovery search path (14/14 top-3, 100%) | **met** |
| Secret logging check | code/test review | no raw arg values logged | no Execute body changes in US-I01..I05; tool argument privacy pattern (names+keys only) unchanged | **met** |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | green across all 5 story commits (009639ae, ab843241, 7400c1f1, 16f4ba01, 28ae9324) | **met** |
| Tool discovery top-k evals | `go test ./internal/agent/tools/index/...` | ≥70% top-3 | 14/14 (100%) — eval_topk_test.go (commit 16f4ba01) | **met** |
| Parameter accuracy evals | `go test ./internal/agent/tools/registry/...` | 0 schema violations | 0 violations across all native tool examples (commit 28ae9324) | **met** |
| active-turn visibility never bypasses authz | `go test ./internal/agent/tools/registry/...` | deny + no Execute | visibility_authz_test.go 3-scenario: unauthorized call returns error, Execute count = 0 (commit 7400c1f1) | **met** |
