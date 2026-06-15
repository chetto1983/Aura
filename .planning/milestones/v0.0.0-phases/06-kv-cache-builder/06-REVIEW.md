---
phase: 06-kv-cache-builder
reviewed: 2026-06-02T10:11:59Z
depth: standard
files_reviewed: 57
files_reviewed_list:
  - .github/workflows/ci.yml
  - cmd/aura/cache.go
  - cmd/aura/cache_audit.go
  - cmd/aura/cache_stats.go
  - cmd/aura/cache_test.go
  - cmd/aura/cachefakes.go
  - cmd/aura/chat.go
  - cmd/aura/chat_test.go
  - cmd/aura/cmdfakes_test.go
  - cmd/aura/main.go
  - internal/agent/llm_agent.go
  - internal/agent/prompt/builder.go
  - internal/agent/prompt/builder_test.go
  - internal/agent/prompt/cache_anthropic.go
  - internal/agent/prompt/hash.go
  - internal/agent/prompt/hash_test.go
  - internal/cachemetrics/store.go
  - internal/cachemetrics/store_helpers.go
  - internal/cachemetrics/store_helpers_test.go
  - internal/db/cache_metrics_integration_test.go
  - internal/db/db_test.go
  - internal/db/migrations/0007_cache_metrics.down.sql
  - internal/db/migrations/0007_cache_metrics.up.sql
  - internal/db/queries/cache_metrics.sql
  - internal/db/sqlc/cache_metrics.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/llm/client.go
  - internal/runner/fakes_test.go
  - internal/runner/interfaces.go
  - internal/runner/runner.go
  - internal/runner/runner_cachemetric_test.go
  - internal/runner/runner_persist.go
  - internal/runner/runner_test.go
  - scripts/cache_invariant_audit.sh
  - scripts/cache_invariant_negative_test.sh
  - scripts/fixtures/cache_invariant/README.md
  - scripts/fixtures/cache_invariant/turn-01.json
  - scripts/fixtures/cache_invariant/turn-02.json
  - scripts/fixtures/cache_invariant/turn-03.json
  - scripts/fixtures/cache_invariant/turn-04.json
  - scripts/fixtures/cache_invariant/turn-05.json
  - scripts/fixtures/cache_invariant/turn-06.json
  - scripts/fixtures/cache_invariant/turn-07.json
  - scripts/fixtures/cache_invariant/turn-08.json
  - scripts/fixtures/cache_invariant/turn-09.json
  - scripts/fixtures/cache_invariant/turn-10.json
  - scripts/fixtures/cache_invariant/turn-11.json
  - scripts/fixtures/cache_invariant/turn-12.json
  - scripts/fixtures/cache_invariant/turn-13.json
  - scripts/fixtures/cache_invariant/turn-14.json
  - scripts/fixtures/cache_invariant/turn-15.json
  - scripts/fixtures/cache_invariant/turn-16.json
  - scripts/fixtures/cache_invariant/turn-17.json
  - scripts/fixtures/cache_invariant/turn-18.json
  - scripts/fixtures/cache_invariant/turn-19.json
  - scripts/fixtures/cache_invariant/turn-20.json
findings:
  critical: 3
  warning: 3
  info: 0
  total: 6
status: issues_found
---

# Phase 06: Code Review Report

**Reviewed:** 2026-06-02T10:11:59Z
**Depth:** standard
**Files Reviewed:** 57
**Status:** issues_found

## Summary

Reviewed the Phase 06 cache builder surface: prompt assembly, cache audit gate, cache metrics persistence, migration/sqlc output, CLI wiring, CI scripts, tests, and replay fixtures. The main risks are a non-atomic assistant-turn/metric write, a false-green cache invariant gate, and shell command injection through the audit wrapper test seam.

Verification run during review: `go test ./...`, `bash scripts/cache_invariant_audit.sh`, and `bash scripts/cache_invariant_negative_test.sh` all passed.

## Critical Issues

### CR-01: Assistant turns can be committed even when the new metric write fails

**Classification:** BLOCKER
**File:** `internal/runner/runner_persist.go:79`
**Issue:** `persistAssistantAnswer` appends the assistant turn at lines 79-90, then writes `cache_metrics` at line 91. If the metric insert fails, `Runner.Turn` returns an error before yielding the final answer, but the assistant response is already stored in the conversation. The user sees a failed turn while the DB contains the answer; retrying the same prompt can create duplicate user/assistant turns and inconsistent history.
**Fix:** Write the assistant turn, conversation aggregate update, and cache metric row in one database transaction, or introduce a single store method that owns the atomic write.

```go
// Sketch: the concrete store should run all three sqlc writes in one db.WithTx.
func (s *Store) AppendAssistantTurnWithCacheMetric(ctx context.Context, turn conversations.AppendTurnParams, metric sqlc.InsertCacheMetricParams) error {
    return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
        if err := q.InsertConversationTurn(ctx, toTurnParams(turn)); err != nil {
            return err
        }
        if err := q.UpdateConversationAggregates(ctx, toAggregateParams(turn)); err != nil {
            return err
        }
        return q.InsertCacheMetric(ctx, metric)
    })
}
```

### CR-02: The cache audit checks only the last LLM request of each turn

**Classification:** BLOCKER
**File:** `cmd/aura/cache_audit.go:143`
**Issue:** `replayAudit` appends `client.LastRequest()` once per fixture turn at line 150. Tool fixtures consume multiple LLM requests in a single `Runner.Turn`; the audit ignores every request except the final one. A prefix mutation that occurs only on the first request of a tool round can pass the CI invariant gate.
**Fix:** Capture and hash every request emitted during each replay turn, then update the wrapper count/labels to match request count rather than assuming exactly 20 requests.

```go
before := len(client.Requests)
if err := drainTurn(r.Turn(ctx, convID, &user)); err != nil {
    return nil, exitFixture
}
reqs = append(reqs, client.Requests[before:]...)
```

### CR-03: The audit wrapper executes an environment-controlled command through eval

**Classification:** BLOCKER
**File:** `scripts/cache_invariant_audit.sh:43`
**Issue:** `AURA_CACHE_AUDIT_CMD` is read as a string and executed via `eval`. Any environment value for that variable is shell code, so a CI/local caller can inject arbitrary commands into the gate process. The negative test currently depends on this injection shape.
**Fix:** Replace the command-string seam with an executable path or temp script path and invoke it without `eval`; update the negative test to write a temporary script that emits the poisoned stream.

```bash
if [[ -n "${AURA_CACHE_AUDIT_BIN:-}" ]]; then
  OUT="$("$AURA_CACHE_AUDIT_BIN" 2>"$ERR_FILE")" || code=$?
else
  OUT="$(go run ./cmd/aura cache-audit 2>"$ERR_FILE")" || code=$?
fi
```

## Warnings

### WR-01: Missing messages[0] can hash as a successful empty prefix

**Classification:** WARNING
**File:** `cmd/aura/cache_audit.go:159`
**Issue:** `hashMessages0` delegates to `PrefixHash(req.Messages, []int{0})`, and `PrefixHash` skips absent indices. If a regression emits requests with no `messages[0]`, every request hashes to the empty SHA-256 digest and the audit can pass even though the system prefix disappeared.
**Fix:** Make the audit path strict for index 0 before hashing.

```go
func hashMessages0(req llm.Request) (string, error) {
    if len(req.Messages) == 0 {
        return "", fmt.Errorf("request is missing messages[0]")
    }
    return prompt.PrefixHash(req.Messages, []int{0})
}
```

### WR-02: Fixture decoding does not enforce the documented response shape

**Classification:** WARNING
**File:** `cmd/aura/cache_audit.go:177`
**Issue:** `decodeFixture` only checks non-empty `user` and `responses`. It accepts response entries with neither `text` nor `tool_calls`, entries with both fields, and tool calls with missing id/name/arguments. `toFakeTurn` then silently treats these as empty text turns or drops the text side, so corrupt fixtures can reduce gate coverage without returning exit 2.
**Fix:** Validate each response during decode: exactly one of `text` or `tool_calls`, non-empty text for text responses, non-empty id/name/arguments for tool calls, and valid JSON in each tool-call `arguments`.

### WR-03: AggregateSince integration coverage can pass with a broken time filter

**Classification:** WARNING
**File:** `internal/db/cache_metrics_integration_test.go:143`
**Issue:** The test derives exact per-conversation sums from `ListSince`, but checks `AggregateSince` only with `>=` assertions because the DB may contain other rows. If `AggregateCacheMetricsSince` stopped applying `WHERE ts >= since`, the aggregate could still pass by including older rows.
**Fix:** Isolate the aggregate window so exact equality can be asserted. One low-friction option is to insert the test rows at a future timestamp window that no other test data uses, then require exact totals.

```go
base := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
cutoff := base.Add(-1 * time.Hour)
// insert rows around cutoff using base...
if agg.TotalPromptTokens != wantPrompt || agg.TotalCachedTokens != wantCached || agg.Turns != 3 {
    t.Fatalf("AggregateSince exact totals mismatch: %+v", agg)
}
```

---

_Reviewed: 2026-06-02T10:11:59Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
