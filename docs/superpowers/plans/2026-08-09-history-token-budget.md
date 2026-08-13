# History Token Budget (Part B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop discarding 22–72% of every long conversation to a 50-row fetch cap that has no relationship to the model's context window.

**Architecture:** The row count stops being the criterion and becomes a work ceiling. A backward token walk — ported from Hermes's `_find_tail_cut_by_tokens` — decides what is retained, against a budget derived from the window (`min(correctnessCap, ContextWindow/2)`). Because the walk goes newest-first, a spilled turn's sidecar is read only if the turn is retained, so exact token counts cost less I/O than today's read-everything-then-discard.

**Tech Stack:** Go 1.25+, Postgres via sqlc, vendored cl100k_base tiktoken (already used by the L1/L2 ladder).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-09-history-token-budget-design.md` §5.
- **Run Part A first.** Part A's breakdown is what verifies Part B did not simply move the loss somewhere invisible.
- **No file over 600 LOC.** `internal/conversations/context.go` is 590 and `store.go` is 574 — new logic goes in `context_budget.go`, not into either.
- **Small windows must not change behaviour.** At `ContextWindow = 32000` the budget equals today's `smallWindowHardCapFloor` (16,000), so the retained set must be byte-identical to current output. This is a test, not a hope.
- **The protected `system` head keeps its protection.** `ListRecentTurnsBySeq` returns it unconditionally and the cut must never drop it.
- **No migration. No new environment variable.** The budget derives from `AURA_MODEL_CONTEXT_WINDOW`, already DB-driven via `aura.settings`.
- Post-edit validation after every Go file edit: `go vet ./...`, `go build ./...`, `go test ./internal/<package>/`, `go test -race ./internal/<package>/`.
- Coverage floor for touched packages: 85%.

---

### Task 1: The history budget

**Files:**
- Create: `internal/conversations/context_budget.go`
- Test: `internal/conversations/context_budget_test.go`

**Interfaces:**
- Consumes: `ContextConfig` and its `hardCap()` (`internal/conversations/context.go:80-131`), `smallWindowHardCapFloor` (`context.go:139-144`).
- Produces: `func (c ContextConfig) HistoryBudget() int`.

- [ ] **Step 1: Write the failing test**

Create `internal/conversations/context_budget_test.go`:

```go
package conversations

import "testing"

func TestHistoryBudget(t *testing.T) {
	tests := []struct {
		name   string
		cfg    ContextConfig
		want   int
		reason string
	}{
		{
			name: "large window is halved",
			cfg:  ContextConfig{ContextWindow: 1_000_000, MaxOutputTokens: 19767},
			want: 500_000,
			reason: "correctness cap is 967,000; half the window is tighter and wins",
		},
		{
			name: "mid window is halved",
			cfg:  ContextConfig{ContextWindow: 200_000, MaxOutputTokens: 8000},
			want: 100_000,
			reason: "correctness cap is 167,000; half the window is tighter",
		},
		{
			name: "small window keeps today's floor",
			cfg:  ContextConfig{ContextWindow: 32_000, MaxOutputTokens: 4000},
			want: 16_000,
			reason: "smallWindowHardCapFloor already returns window/2, so the two coincide",
		},
		{
			name: "degenerate window disables the cut",
			cfg:  ContextConfig{ContextWindow: 0},
			want: 0,
			reason: "no usable budget; the work ceiling is the only bound",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.HistoryBudget(); got != tc.want {
				t.Fatalf("HistoryBudget() = %d, want %d (%s)", got, tc.want, tc.reason)
			}
		})
	}
}

func TestHistoryBudgetNeverExceedsCorrectnessCap(t *testing.T) {
	// A reservation larger than half the window must not let the budget exceed
	// the cap the provider would reject.
	cfg := ContextConfig{ContextWindow: 50_000, MaxOutputTokens: 40_000}
	if got, cap := cfg.HistoryBudget(), cfg.HardCap(); got > cap {
		t.Fatalf("HistoryBudget() = %d exceeds HardCap() = %d", got, cap)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversations/ -run TestHistoryBudget -v`
Expected: FAIL — `cfg.HistoryBudget undefined`

- [ ] **Step 3: Write minimal implementation**

Create `internal/conversations/context_budget.go`:

```go
package conversations

// HistoryBudget is how many tokens of conversation history may occupy the window.
// It is deliberately TIGHTER than hardCap: the cap is a correctness bound (exceed
// it and the provider errors), while this is a quality bound. Chroma's context-rot
// research measures accuracy falling with input length even on trivial tasks, and a
// single distractor lowering it against a no-distractor baseline. The 50% figure is
// this project's choice on top of that evidence, not a number the research
// prescribes — see the 2026-08-09 spec.
//
// window/2 is not a new constant: smallWindowHardCapFloor already returns it, so on
// small windows the two coincide exactly and behaviour is unchanged. A non-positive
// result means "no usable budget", which disables the token cut and leaves the row
// work-ceiling as the only bound — the same way hardCap == 0 disables the L2 gate.
func (c ContextConfig) HistoryBudget() int {
	cap := c.hardCap()
	if half := c.ContextWindow / 2; half < cap {
		return half
	}
	return cap
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversations/ -run TestHistoryBudget -v`
Expected: PASS

Run: `go vet ./... && go build ./... && go test -race ./internal/conversations/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/conversations/context_budget.go internal/conversations/context_budget_test.go
git commit -m "Derive the history budget from the window, since 50 rows never knew it"
```

---

### Task 2: The backward token cut

Port `_find_tail_cut_by_tokens` (`agent/context_compressor.py:5119`). Pure over an injected sizer so it needs no database and no filesystem.

**Files:**
- Modify: `internal/conversations/context_budget.go`
- Test: `internal/conversations/context_budget_test.go`

**Interfaces:**
- Consumes: `Turn` (`internal/conversations`), `llm.RoleSystem`/`RoleUser`/`RoleTool`/`RoleAssistant`.
- Produces:
  - `type turnSizer func(Turn) (int, error)`
  - `func cutToBudget(turns []Turn, budget, minTurns int, size turnSizer) ([]Turn, error)`

Contract, all four invariants from the spec:
1. the token budget is the primary criterion;
2. `minTurns` non-system turns are retained even when the budget is exhausted;
3. a single turn larger than the budget is retained whole, up to a soft ceiling of 1.5× budget;
4. the cut never lands between an assistant turn carrying `tool_calls` and the `role='tool'` turns answering it, and the newest user turn is always retained.

A leading `role='system'` turn at index 0 is protected: it is always retained and never counted against `minTurns`.

- [ ] **Step 1: Write the failing test**

Append to `internal/conversations/context_budget_test.go`:

```go
func sized(n int) turnSizer {
	return func(Turn) (int, error) { return n, nil }
}

func turnsOf(roles ...string) []Turn {
	out := make([]Turn, 0, len(roles))
	for i, role := range roles {
		out = append(out, Turn{Seq: i + 1, Role: role})
	}
	return out
}

func seqs(turns []Turn) []int {
	out := make([]int, 0, len(turns))
	for _, t := range turns {
		out = append(out, t.Seq)
	}
	return out
}

func TestCutToBudgetKeepsNewestWithinBudget(t *testing.T) {
	// 6 turns of 10 tokens, budget 30 → the newest 3 fit.
	turns := turnsOf("user", "assistant", "user", "assistant", "user", "assistant")

	got, err := cutToBudget(turns, 30, 1, sized(10))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{4, 5, 6}; !equalInts(seqs(got), want) {
		t.Fatalf("kept %v, want %v", seqs(got), want)
	}
}

func TestCutToBudgetAlwaysKeepsSystemHead(t *testing.T) {
	turns := append(turnsOf("system"), turnsOf("user", "assistant", "user")...)
	for i := range turns {
		turns[i].Seq = i + 1
	}
	turns[0].Role = "system"

	got, err := cutToBudget(turns, 10, 1, sized(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Role != "system" {
		t.Fatalf("system head dropped: %v", seqs(got))
	}
}

func TestCutToBudgetHonoursMessageFloor(t *testing.T) {
	// Budget 0 must still retain minTurns recent turns.
	turns := turnsOf("user", "assistant", "user", "assistant")

	got, err := cutToBudget(turns, 0, 2, sized(1000))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("kept %d turns, want at least the floor of 2", len(got))
	}
}

func TestCutToBudgetKeepsOversizedTurnWhole(t *testing.T) {
	// One turn of 120 against a budget of 100: under the 1.5x soft ceiling, so it
	// is retained whole rather than dropped, leaving the model something to answer.
	turns := turnsOf("user")

	got, err := cutToBudget(turns, 100, 1, sized(120))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("kept %d turns, want the oversized turn retained whole", len(got))
	}
}

func TestCutToBudgetNeverSplitsAToolGroup(t *testing.T) {
	// assistant(tool_calls) -> tool -> tool. A cut landing on either tool turn
	// would orphan it from the assistant turn that requested it.
	turns := []Turn{
		{Seq: 1, Role: "user"},
		{Seq: 2, Role: "assistant", ToolCalls: []byte(`[{"id":"c1"}]`)},
		{Seq: 3, Role: "tool", ToolCallID: "c1"},
		{Seq: 4, Role: "tool", ToolCallID: "c1"},
	}

	got, err := cutToBudget(turns, 25, 1, sized(10))
	if err != nil {
		t.Fatal(err)
	}
	for i, turn := range got {
		if turn.Role == "tool" && i == 0 {
			t.Fatalf("cut orphaned a tool turn: %v", seqs(got))
		}
	}
}

func TestCutToBudgetKeepsNewestUserTurn(t *testing.T) {
	turns := turnsOf("user", "assistant", "user")

	got, err := cutToBudget(turns, 1, 1, sized(10))
	if err != nil {
		t.Fatal(err)
	}
	var sawUser bool
	for _, turn := range got {
		if turn.Role == "user" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatalf("newest user turn dropped: %v", seqs(got))
	}
}

func TestCutToBudgetZeroBudgetDisablesTheCut(t *testing.T) {
	turns := turnsOf("user", "assistant", "user", "assistant")

	got, err := cutToBudget(turns, 0, 0, sized(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(turns) {
		t.Fatalf("kept %d, want all %d when the budget is disabled", len(got), len(turns))
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/conversations/ -run TestCutToBudget -v`
Expected: FAIL — `undefined: cutToBudget`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/conversations/context_budget.go`:

```go
// softCeilingNumerator/Denominator express Hermes's 1.5x allowance: a single
// message may push the retained set past the budget rather than be cut in half,
// because half a tool result is worse than a slightly larger prompt.
const (
	softCeilingNumerator   = 3
	softCeilingDenominator = 2
)

// turnSizer returns a turn's token cost. It is injected so the cut is pure: the
// production sizer rehydrates a spilled turn's sidecar, which is exactly why the
// walk goes newest-first — an excluded turn is never read from disk.
type turnSizer func(Turn) (int, error)

// cutToBudget retains the newest turns that fit budget, porting the four
// invariants of Hermes's _find_tail_cut_by_tokens
// (agent/context_compressor.py:5119):
//
//  1. the token budget is the primary criterion;
//  2. minTurns non-system turns survive even when the budget is exhausted;
//  3. a turn larger than the budget is retained whole up to a 1.5x soft ceiling,
//     rather than splitting it;
//  4. the cut never separates an assistant turn's tool_calls from the role='tool'
//     turns answering them, and the newest user turn is always retained.
//
// A leading system turn is always retained and never counted against minTurns. A
// non-positive budget disables the cut entirely: the caller's row ceiling is then
// the only bound, matching how hardCap == 0 disables the L2 gate.
func cutToBudget(turns []Turn, budget, minTurns int, size turnSizer) ([]Turn, error) {
	if budget <= 0 || len(turns) == 0 {
		return turns, nil
	}

	head, body := splitProtectedHead(turns)

	headTokens := 0
	for _, turn := range head {
		n, err := size(turn)
		if err != nil {
			return nil, err
		}
		headTokens += n
	}

	softCeiling := budget * softCeilingNumerator / softCeilingDenominator
	accumulated := headTokens
	start := len(body) // index of the first retained body turn

	for i := len(body) - 1; i >= 0; i-- {
		n, err := size(body[i])
		if err != nil {
			return nil, err
		}
		kept := len(body) - i
		if accumulated+n > softCeiling && kept > minTurns && start < len(body) {
			break
		}
		accumulated += n
		start = i
	}

	start = alignToTurnBoundary(body, start)

	out := make([]Turn, 0, len(head)+len(body)-start)
	out = append(out, head...)
	out = append(out, body[start:]...)
	return out, nil
}

// splitProtectedHead peels the leading system turn, which is retained
// unconditionally and never counted against the message floor.
func splitProtectedHead(turns []Turn) (head, body []Turn) {
	if len(turns) > 0 && turns[0].Role == roleSystem {
		return turns[:1], turns[1:]
	}
	return nil, turns
}

// alignToTurnBoundary moves a cut point forward off any role='tool' turn, so the
// retained set never opens with a tool result whose requesting assistant turn was
// dropped — a shape strict providers reject.
func alignToTurnBoundary(body []Turn, start int) int {
	for start < len(body) && body[start].Role == roleTool {
		start++
	}
	return start
}
```

Add the two role constants beside the others in this file if the package does not already export equivalents — check `internal/conversations/context.go` first and reuse whatever it uses (`llm.RoleSystem`, `llm.RoleTool`) rather than introducing new names:

```go
const (
	roleSystem = llm.RoleSystem
	roleTool   = llm.RoleTool
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversations/ -run TestCutToBudget -v`
Expected: PASS (7 tests)

Run: `go vet ./... && go build ./... && go test -race ./internal/conversations/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/conversations/context_budget.go internal/conversations/context_budget_test.go
git commit -m "Cut history by tokens walking backward, so an excluded turn costs no read"
```

---

### Task 3: Raise the work ceiling

Without this the cut has nothing to cut: the SQL still returns 50 rows and the budget can only trim further, leaving the measured 22–72% loss untouched.

**Files:**
- Modify: `internal/config/config.go:417`
- Modify: `internal/config/config_knobs.go:98`
- Modify: `.env:56`
- Modify: `compose.yaml:104`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `envutil.IntDefault`.
- Produces: `AURA_HISTORY_HARD_CAP_TURNS` default `1000`.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestHistoryHardCapDefaultIsTheWorkCeiling(t *testing.T) {
	t.Setenv("AURA_HISTORY_HARD_CAP_TURNS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HistoryHardCapTurns != 1000 {
		t.Fatalf("HistoryHardCapTurns = %d, want 1000; at 50 the token budget can only trim further and the row cap stays the real bound",
			cfg.HistoryHardCapTurns)
	}
}
```

If `Load()` requires more environment than a bare test provides, mirror the setup used by the neighbouring tests in that file rather than inventing one.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestHistoryHardCapDefaultIsTheWorkCeiling -v`
Expected: FAIL — `HistoryHardCapTurns = 50, want 1000`

- [ ] **Step 3: Write minimal implementation**

`internal/config/config.go:417`:

```go
		HistoryHardCapTurns:        envutil.IntDefault("AURA_HISTORY_HARD_CAP_TURNS", 1000),
```

`internal/config/config_knobs.go:98`:

```go
		{Name: "AURA_HISTORY_HARD_CAP_TURNS", Kind: KindInt, Default: "1000"},
```

`internal/conversations/context.go:36`:

```go
	defaultHistoryHardCapTurns = 1000
```

`.env:56`:

```
AURA_HISTORY_HARD_CAP_TURNS=1000
```

`compose.yaml:104`:

```yaml
      AURA_HISTORY_HARD_CAP_TURNS: ${AURA_HISTORY_HARD_CAP_TURNS:-1000}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ ./internal/conversations/ -v`
Expected: PASS. Any existing test asserting `50` is asserting the defect — update it and say so in its comment.

Run: `go vet ./... && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_knobs.go internal/config/config_test.go internal/conversations/context.go .env compose.yaml
git commit -m "Make the row cap a work ceiling, not the thing deciding what the model sees"
```

---

### Task 4: Cut before rehydrating sidecars

Today `loadRecentTurns` reads the sidecar for every fetched row (`internal/conversations/store.go:454-470`), including rows the ladder then discards. Inverting that is what makes a 1000-row ceiling affordable.

**Files:**
- Modify: `internal/conversations/store.go:428-472`
- Modify: `internal/conversations/store_branch.go:103-140`
- Test: `internal/conversations/store_test.go`

**Interfaces:**
- Consumes: `cutToBudget` (Task 2), `ContextConfig.HistoryBudget()` (Task 1), `Store.readTurnSidecar(conversationID string, seq int) ([]byte, error)` (`store.go:484`).
- Produces: `loadRecentTurns` and `loadRecentBranchTurns` gain a `cfg ContextConfig` parameter and return only retained turns, with sidecars rehydrated for those turns alone.

- [ ] **Step 1: Write the failing test**

Add to `internal/conversations/store_test.go`:

```go
func TestSidecarsRehydrateOnlyForRetainedTurns(t *testing.T) {
	// A spilled turn outside the budget must never be read from disk. The sizer
	// counts reads; an excluded turn contributing a read is the regression.
	var reads []int
	size := func(turn Turn) (int, error) {
		if turn.ContentSidecarPath != "" {
			reads = append(reads, turn.Seq)
		}
		return 10, nil
	}
	turns := []Turn{
		{Seq: 1, Role: "user", ContentSidecarPath: "1.content"},
		{Seq: 2, Role: "assistant", ContentSidecarPath: "2.content"},
		{Seq: 3, Role: "user"},
	}

	got, err := cutToBudget(turns, 15, 1, size)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seq != 3 {
		t.Fatalf("kept %v, want only seq 3", seqs(got))
	}
	for _, seq := range reads {
		if seq == 1 {
			t.Errorf("seq 1 was read from disk despite being outside the budget")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversations/ -run TestSidecarsRehydrateOnly -v`
Expected: PASS if Task 2 is correct — this test pins the *property* Task 4 relies on. If it fails, fix `cutToBudget` before proceeding; the store change is unsafe without it.

- [ ] **Step 3: Rewrite the loader**

In `internal/conversations/store.go`, replace the body of `loadRecentTurns` after the query so rows are NOT rehydrated in the loop. Build plain turns, then cut with a sizer that rehydrates on demand:

```go
	out := make([]Turn, 0, len(rows))
	for _, row := range rows {
		out = append(out, turnFromRow(sqlc.ListTurnsBySeqRow(row)))
	}
	return s.cutLoadedTurns(conversationID, out, cfg)
```

Add to `internal/conversations/context_budget.go`:

```go
// cutLoadedTurns applies the history budget to freshly queried rows, rehydrating a
// spilled turn's sidecar ONLY when the turn survives the cut. The walk is
// newest-first, so an excluded turn costs no read — which is what makes a large row
// ceiling affordable. A missing sidecar for a retained turn stays a hard error,
// exactly as it was when every row was read eagerly.
func (s *Store) cutLoadedTurns(conversationID string, turns []Turn, cfg ContextConfig) ([]Turn, error) {
	hydrated := make(map[int]string, len(turns))
	size := func(turn Turn) (int, error) {
		content := turn.Content
		if turn.ContentSidecarPath != "" {
			cached, ok := hydrated[turn.Seq]
			if !ok {
				data, err := s.readTurnSidecar(conversationID, turn.Seq)
				if err != nil {
					return 0, fmt.Errorf("load recent turns %s seq %d: read sidecar: %w",
						conversationID, turn.Seq, err)
				}
				cached = string(data)
				hydrated[turn.Seq] = cached
			}
			content = cached
		}
		return CountTokens(content) + CountTokens(string(turn.ToolCalls)) + CountTokens(turn.ToolCallID), nil
	}

	kept, err := cutToBudget(turns, cfg.HistoryBudget(), messageFloor(cfg), size)
	if err != nil {
		return nil, err
	}
	for i := range kept {
		if kept[i].ContentSidecarPath != "" {
			kept[i].Content = hydrated[kept[i].Seq]
		}
	}
	return kept, nil
}
```

and the floor helper beside it:

```go
// messageFloor is Hermes's protect_last_n: the number of recent non-system turns
// kept verbatim even when the budget is exhausted, so a tiny window still leaves
// the model something to answer from. ToolEvictAfterTurns (10 by default) is the
// existing knob for "how many recent turns are special", reused rather than
// introducing a second one. normalizeHistoryTurnCap is deliberately NOT used here:
// it normalizes a ROW ceiling into [4, 1000] with a default of 1000, which is a
// different quantity that happens to be an int.
func messageFloor(cfg ContextConfig) int {
	return max(3, cfg.ToolEvictAfterTurns)
}
```

Thread `cfg ContextConfig` into `loadRecentTurns` and `loadRecentBranchTurns` and their two call sites in `context.go:227-235` and `context.go:255-264`. Apply the identical `cutLoadedTurns` call in `store_branch.go` so the branch path cannot diverge from the linear one.

**`ListRecentTurnsBySeq` needs no edit.** Its `hard_cap` is already a bound parameter (`internal/db/queries/conversation_turns.sql:26-62`), so Task 3's default change is the whole SQL-side change. The spec's file table lists the query as touched; that was written before the parameter was checked. Leave the SQL alone and correct the spec's table in this task's commit.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversations/ -v`
Expected: PASS

Run: `go vet ./... && go build ./... && go test -race ./internal/conversations/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/conversations/store.go internal/conversations/store_branch.go internal/conversations/context.go internal/conversations/context_budget.go internal/conversations/store_test.go
git commit -m "Read a spilled turn only when it survives the cut, not before deciding"
```

---

### Task 5: Small-window equivalence

The riskiest property in this change: behaviour on small windows must not move at all.

**Files:**
- Test: `internal/conversations/context_budget_test.go`

- [ ] **Step 1: Write the test**

```go
func TestSmallWindowRetainedSetUnchanged(t *testing.T) {
	// At 32k the budget (16,000) equals today's smallWindowHardCapFloor, so the
	// retained set must match what the pre-change ladder produced. Any divergence
	// here is a regression for every small-window deployment.
	cfg := ContextConfig{ContextWindow: 32_000, MaxOutputTokens: 4000, ToolEvictAfterTurns: 10}
	if got, want := cfg.HistoryBudget(), smallWindowHardCapFloor(32_000); got != want {
		t.Fatalf("HistoryBudget() = %d, want smallWindowHardCapFloor = %d", got, want)
	}

	turns := make([]Turn, 0, 40)
	for i := 1; i <= 40; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		turns = append(turns, Turn{Seq: i, Role: role})
	}

	got, err := cutToBudget(turns, cfg.HistoryBudget(), 10, sized(500))
	if err != nil {
		t.Fatal(err)
	}
	// 16,000 / 500 = 32 turns fit; the soft ceiling allows up to 48, so all 40 stay.
	if len(got) != 40 {
		t.Fatalf("kept %d of 40 turns at a small window", len(got))
	}
}
```

- [ ] **Step 2: Run and confirm**

Run: `go test ./internal/conversations/ -run TestSmallWindow -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/conversations/context_budget_test.go
git commit -m "Pin small-window behaviour, the property this change must not move"
```

---

### Task 6: Integration test against a real database

**Files:**
- Test: `internal/conversations/context_budget_integration_test.go` (build tag `db_integration`)

- [ ] **Step 1: Write the test**

```go
//go:build db_integration

package conversations

import (
	"context"
	"testing"
)

func TestLongConversationLoadsCompletelyAtLargeWindow(t *testing.T) {
	store, convID := seedConversation(t, 120) // existing helper; if absent, write it here
	cfg := ContextConfig{ContextWindow: 1_000_000, MaxOutputTokens: 19767,
		ToolEvictAfterTurns: 10, HistoryHardCapTurns: 1000}

	msgs, err := store.LoadManagedHistory(context.Background(), convID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 120 seeded turns are far inside a 500,000-token budget: none may be dropped.
	if len(msgs) < 120 {
		t.Fatalf("loaded %d messages of 120 seeded; the row cap is still deciding", len(msgs))
	}
}

func TestLongConversationCutsAtSmallWindow(t *testing.T) {
	store, convID := seedConversation(t, 120)
	cfg := ContextConfig{ContextWindow: 8_000, MaxOutputTokens: 1000,
		ToolEvictAfterTurns: 10, HistoryHardCapTurns: 1000}

	msgs, err := store.LoadManagedHistory(context.Background(), convID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) >= 120 {
		t.Fatalf("loaded %d messages at a 4,000-token budget; the cut did not fire", len(msgs))
	}
}
```

Follow the existing `db_integration` conventions in this package for store construction and identity scoping — read a neighbouring tagged test first and mirror it. Per CLAUDE.md the skip helper must `t.Fatal` under `$CI`, never skip.

- [ ] **Step 2: Run against the live stack**

```bash
make db-migrate
go test -tags db_integration ./internal/conversations/ -run TestLongConversation -v
```

Expected: PASS, with a runtime well above a second. A sub-second result means the tier skipped itself.

- [ ] **Step 3: Commit**

```bash
git add internal/conversations/context_budget_integration_test.go
git commit -m "Prove the budget decides against a real database, not just a fake sizer"
```

---

### Task 7: Live verification — the regression measure from the spec

**Files:**
- Create: `docs/superpowers/verification/2026-08-09-history-token-budget/RESULTS.md`

- [ ] **Step 1: Rebuild and restart from branch HEAD**

```bash
docker compose build aura && docker compose up -d aura
```

- [ ] **Step 2: Re-run the spec's §2.1 measurement with real tokens**

Use the tokenizer, not char/4. Dump the four conversations' turns and count via llama.cpp:

```bash
docker exec aura-postgres psql -U aura -d aura -t -A -c \
 "SELECT json_agg(row_to_json(x))::text FROM (
    SELECT conversation_id::text cid, seq, role, coalesce(content,'') content,
           coalesce(tool_calls::text,'') tool_calls, coalesce(tool_call_id,'') tool_call_id
    FROM aura.conversation_turns ORDER BY conversation_id, seq) x" > turns.json
```

then count each turn's `content + tool_calls + tool_call_id` through `POST http://127.0.0.1:8084/tokenize`, summing per conversation, with the retained set now computed by the new loader rather than by `row_number() <= 50`.

Expected, from spec §2.1: tokens lost is **0** for all four — `019fa8ba` 26,933 → 0, `019fa501` 22,633 → 0, `019f83d0` 10,308 → 0, `019fdb4c` 2,971 → 0.

- [ ] **Step 3: Confirm through Part A's instrument**

```bash
docker exec aura-prometheus-1 sh -c "wget -qO- http://127.0.0.1:9464/metrics" | grep '^aura_agent_context_tokens'
```

Drive one turn on the existing conversation `019fa8ba` and assert `aura_agent_context_tokens{category="conversation"}` is now in the tens of thousands rather than the ~11,000 the 50-row window allowed. **This is why Part A ships first**: without it, "the loss is fixed" would be an assertion about a SQL query rather than about what reached the model.

- [ ] **Step 4: Confirm nothing regressed at the correctness cap**

```bash
docker exec aura-postgres psql -U aura -d aura -c \
  "SELECT count(*) FROM aura.context_rot_events WHERE ts > now() - interval '1 hour'"
```

Expected: `0` new rows. L2.5 firing on a 1M window would mean the budget is wrong, not that the fix worked.

- [ ] **Step 5: Write the results and commit**

Record every figure, including any that did not reach 0 and why.

```bash
git add docs/superpowers/verification/2026-08-09-history-token-budget/RESULTS.md
git commit -m "Verify the 72% is back, measured the way the spec said to measure it"
```

---

## Definition of Done

- [ ] `go vet ./... && go build ./...` clean
- [ ] `go test -race ./internal/conversations/ ./internal/config/` green
- [ ] `go test -tags db_integration ./internal/conversations/` green with a real runtime
- [ ] `bash scripts/coverage_docker.sh` — touched packages at or above 85%
- [ ] Task 7: tokens lost is 0 for all four measured conversations, verified with the tokenizer
- [ ] Task 7: `aura_agent_context_tokens{category="conversation"}` confirms the history actually reached the model
- [ ] No new `context_rot_events` rows on a 1M window
- [ ] `make quality` green before push
