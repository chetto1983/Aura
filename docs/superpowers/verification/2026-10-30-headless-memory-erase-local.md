# A run with no operator cannot erase memory — local verification, 2026-10-30

Spec: `docs/superpowers/specs/2026-10-30-headless-memory-erase-design.md`.
Plan: `docs/superpowers/plans/2026-10-30-headless-memory-erase.md`.
Where: a cloud container with Go 1.27.3, and Docker 29.9.2, started by hand once the operator
pointed out it was installed. **Not the lab VM.**

## Step 0, in-process, before the change (`de1c428`)

Two throwaway probes, not committed, reproduced the gateway's verdict on the bridge's real output:

| Probe | Result |
|---|---|
| `specFromToolDefWithPolicy("memory", memory_forget{DestructiveHint:false}, memory recipe)` | `Name=memory__memory_forget Mutating=true Destructive=false recipe=recipe:memory/memory_forget` |
| the same for `memory_batch{DestructiveHint:true}` | `Mutating=true Destructive=true` |
| `Decide`, `single_user_hardened`, no responder, `{"entity":"throwaway"}`, the spec above | `verdict=allow tier=normal reason="" approval=false reserves=1` |

So, before the change, a headless run on a strict profile erased memory with no approval.

## After the change (`d2c1497` and the test commit after it)

| Check | Result |
|---|---|
| `go vet ./...`, `go build ./...` | clean |
| `go test -race` on `internal/gateway`, `internal/agent/mcptools`, `cmd/arcadedb-mcp`, `internal/agent/tools`, `cmd/aura`, `internal/runner`, `internal/cron/...` | all `ok` |
| `golangci-lint` v2.14.0 (built here with Go 1.27.3) on the three touched packages | 0 issues |
| `TestAHeadlessRunCannotEraseMemory` | deny, `Destructive`, "no interactive approver", nothing reserved |
| `TestAHeadlessRunMayCountWhatAnErasureWouldTake` | allow, `Safe`, no approval |
| `TestAnInteractiveErasureAsksTheOperator` | approval request |
| `TestTheBridgedMemoryForgetIsDestructiveWhateverTheServerDeclares` | red before the table change with `DestructiveHint: false`; green after |
| `assertErasingToolsAreDestructive` in `TestToolManifestMatchesServer` | fails with `DestructiveHint: false` even after the golden is regenerated; passes with `true` |
| Mutation, `scripts/critical_mutation_gate.py --measure-scopes gateway` (`classify.go`) | 80.00% (16 killed, 4 survived) |
| Mutation, `go-mutesting internal/agent/mcptools/bridge_risk.go` | 92.45% (49 killed, 4 survived), up from 88.68% before the test commit |
| Unit coverage, `go test -cover` | `internal/gateway` 89.5%, `internal/agent/mcptools` 93.6% |
| Coverage gate, `bash scripts/coverage_docker.sh` on `ff6aa9a`, stack as CI's coverage job (`make db-migrate memory-up-core` with `.github/compose.ci-cache.yaml`), disposable `aura_cov` | **passed**: 50042/57111 = 87.62% ≥ 85%, tier `db_integration`, package policy passed (96 of 104 at target); `internal/gateway` 534/569 = 93.85%, `internal/agent/mcptools` 1375/1466 = 93.79% |
| `go test -race -tags arcadedb_integration -run ^TestAgentMemoryMCPLive ./cmd/arcadedb-mcp/` (agent_memory_eval.py's own command), live ArcadeDB and EmbeddingGemma | ok |
| `TestForgetDetachesOneActorsSourceAfterD10SplitLeavesTheOther`, `TestMemoryCrossDenyThroughTheMCPBoundary`, live | pass |
| The whole of `cmd/arcadedb-mcp` with `-tags arcadedb_integration` | 9 live tests failed with "the hosted route has no credential", the same 9 on `27bb9d5`: `TestApplyBootSettingsFailsClosed` leaked `AURA_EMBED_MODEL` into the process env. Fixed in `e05fed9`; the whole package now passes |

**Survivors.**
- **`classify.go`.** All four are the existing "unparseable JSON → Risky" returns of the skill,
  pack, task and calendar classifiers. They are near-equivalent: an empty action also falls to
  Risky. None touches the new branch.
- **`bridge_risk.go`, in the new function.** One survivor: removing `return false` after a failed
  decode. It is equivalent: the map stays nil and the key lookup fails the same way.
- **`bridge_risk.go`, existing code.** Three survivors: the annotation log line (two) and
  `managedBridgePolicy`'s early return.

## What this does not show

- **The lab VM.** Step 0 and acceptance A4 of the spec are still owed:
  - a real `agent_job` against a real ArcadeDB tenant;
  - the interactive prompt in the cockpit;
  - an `always` grant silencing the next erasure.
- **The delegated coverage authorities.** `internal/arcadedb` and `internal/sandbox/usersandbox`
  are measured by their own reports (`arcadedb_coverage`, `docker_coverage`), which were not run
  here; this change touches neither package.
- **Model behaviour.** Whether a model follows an injected "forget" was not measured.
- **The other memory writes.** `memory_merge_entities` and `memory_reembed` were not re-read.
