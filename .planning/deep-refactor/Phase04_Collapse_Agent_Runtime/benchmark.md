# Phase04 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Call graph/caller search | `rg -n "type Runner|NewRunner|\*agent\.Runner|agentRunner|runner\.go|runner_test\.go" cmd internal -g "*.go"` | no hidden production caller remains | only `internal/agent/runtask_test.go` comment mentions moved runner tests; no production reference | met |
| Prompt snapshots | deterministic snapshot tests | stable ordering and content | deferred follow-up | not part of Phase-G closure |
| Runtime package tests | selected agent/chat/swarm/cron tests | green | Phase-G US-G01..US-G07 shipped; current CI later passed on `ecb4cf3e` | met |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | Phase-G closure passed; current pushed CI run `25958870299` also passed build/vet/test/race gates | met |
