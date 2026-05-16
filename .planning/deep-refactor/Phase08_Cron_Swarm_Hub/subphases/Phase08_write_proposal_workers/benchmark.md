# Phase-S Benchmark

## Performance

| Metric                                          | Result      | Target     |
|-------------------------------------------------|-------------|------------|
| 2 write_proposal subagents complete (fake LLM)  | ~0.25 s     | < 60 s     |
| propose_patch tool round-trip (SQLite insert)   | < 5 ms      | < 100 ms   |
| SweepStaleProposals (3-row DB)                  | < 1 ms      | < 1 s      |

## Test coverage

| Criterion                                                    | Status |
|--------------------------------------------------------------|--------|
| TestParentSpawnsTwoWriteProposalSubagentsAndCollectsProposals | PASS   |
| 2 pending proposed_updates rows (wiki + user_memory)         | PASS   |
| aggregated markdown references both proposal handles         | PASS   |
| wiki_page in write_proposal allowlist → BLOCKED              | PASS   |
| TestSweepStaleProposals: fresh-preserved                     | PASS   |
| TestSweepStaleProposals: 31-day-old purged                   | PASS   |
| TestSweepStaleProposals: approved-never-purged               | PASS   |
| go build ./... green                                         | PASS   |
| go vet ./... green                                           | PASS   |
| go test ./... green (all 80+ packages)                       | PASS   |
