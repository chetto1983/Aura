# M-03 — small-window `hardCap` behavior: industrial research (2026-06-13)

**Question.** Aura's `ContextConfig.hardCap()` (`internal/conversations/context.go:73`) computes
`hard_cap = ContextWindow − max(MaxOutputTokens, 20000) − 13000`, clamped to 0. Any model whose
window < ~33k (the 20k output floor + 13k headroom) gets `hardCap == 0`, and the ladder then
returns **raw history with L2/L2.5 protection entirely OFF** (`context.go:153`). That is the
small-window / local-vLLM case (Slice 13). The behavior is test-locked
(`context_boundary_test.go` `TestLadder_HardCapGate_AtAndAbove` part (b)). What is the industrial
best practice?

## Findings (web + 5 curated `d:/tmp` reference repos)

| Source | Output reservation | Small-window behavior |
|---|---|---|
| **Codex** (`codex-rs`) | flat **95% usable / 5% reserved** (`openai_models.rs:342-344`, applied `turn_context.rs:152-159`); folds system+tools+output into one ratio | **Scales proportionally** — `window·95/100`, no fixed subtraction, can never underflow. No floor, no error. Compaction triggers at 90% (`turn.rs:768`). |
| **nanobot** (`agent/loop.py`, `runner.py`, `memory.py`) | same shape as Aura: `window − max_output − K`, but **K = 1024** (not 13000) | **Clamps to a floor** `max(128, window//2)` when budget ≤0 (`loop.py:633`) — never returns raw history. Char-cap backstop 16k (`memory.py:621-633`). Config-time **min window 4096** (`tools/self.py:96`). |
| picobot | none (message count `MaxHistorySize=50`) | n/a |
| adk-go | none (delegates to provider) | implicit error TODO on truncated output (`base_flow.go:119-123`) |
| agent-memory | n/a (graph store, not a loop context manager) | n/a |
| Published (Claude API, getmaxim, Redis, mem0) | "target 60–70% of advertised window as working max"; "reserve 30–50% headroom" | scale proportionally; fall back to a longer-context model; never silently disable |

**Ranked robustness of the small-window fix:** (1) scale proportionally (Codex) — degrades
smoothly; (2) clamp to a floor `window/2` (nanobot) — keeps the honest fixed-overhead subtraction;
(3) config-time min-window validation; (4) error/refuse (only adk-go, implicit); (5) silently
disable protection — **done by nobody**.

## Decision for Aura

Adopt the **nanobot-style floor** over Codex's flat 95%, because Aura's system prompt + deferred
tool manifest is a large *fixed* overhead — a flat 5% reservation under-reserves on a small window,
while the existing `−max(out,20000)−13000` subtraction is honest about that fixed cost. Keep the
SPEC Req#10 formula for normal/large windows (unchanged there); when it would produce `hardCap ≤ 0`,
clamp to a floor (`ContextWindow/2`) so L2.5 stays active. Optionally reject `ContextWindow` below a
hard minimum at boot (`Config.Validate()`, O-04 already added the seam).

**Blast radius (Wave-2 implementation note).** The `windowFor(want)` test helper
(`context_boundary_test.go:26`) currently encodes `hardCap == want` via `ContextWindow = want+33000,
MaxOutputTokens = 1`. With the floor, that invariant only holds when the formula path wins; for the
small `want` the boundary tests use, the helper must be redefined to `ContextWindow = 2·want` so the
floor path yields `hardCap == want` exactly. The `TestLadder_HardCapGate_AtAndAbove` part (b)
"`hardCap==0` returns full history" sub-case is the test-lock and must be rewritten with
justification: a window too small for the fixed reservation now falls back to a floor and **still
protects**, rather than disabling L2.5. This is a Req#10 semantics change → small PRD-amendment note
before the code commit (PRD-first principle).
