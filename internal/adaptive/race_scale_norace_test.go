//go:build db_integration && !race

package adaptive

// snapshotTimeoutScale is the multiplier applied to the capacity tests' wall-clock bounds.
//
// Those bounds exist to catch a persistence path that degrades non-linearly with snapshot
// cardinality — not to benchmark the machine. Under `-race` every memory access is
// instrumented, so the same work takes several times longer while the SQL it issues is
// unchanged; measuring the instrumented number against the clean bound asserts nothing
// about production. It bit in the memory job, where `-race` runs alongside the graph store,
// the embedding sidecar and the memory MCP on the same 2-vCPU runner: a 32,768-outcome
// Save took 12.7s of the 20s budget and Load then died on the context deadline. The same
// test passes unscaled in the Skills coverage gate, which does not use -race.
//
// Postgres is a separate process and is NOT slowed by the detector, so the per-statement
// timeout the capacity test pins (8s) deliberately stays unscaled — that one is a genuine
// database-side bound and must keep failing loudly.
const snapshotTimeoutScale = 1
