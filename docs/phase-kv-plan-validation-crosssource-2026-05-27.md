# Phase-KV Plan Validation: Cross-Source Code Review
Date: 2026-05-27
Breadth: VERY THOROUGH -- read actual production code, not research summaries

---

## 1. Pattern Parity Check

### nanobot (openai_compat_provider.py)
Status: MATCHES PLAN WITH MINOR DIVERGENCE
nanobot's 4-breakpoint strategy (system + last-tool + MCP-boundary + 2nd-to-last user) is operationally identical to Aura's A03 plan. The research doc claimed the 4th breakpoint goes on system[-1] (last system), but the code (openai_compat_provider.py:410-413) places it on messages[0] (first system) + messages[-2] (2nd-to-last overall message). Aura's plan A03 proposes system breakpoint on the first system message post-A02a split, which matches nanobot's actual implementation on messages[0], not the last.

Verdict: MATCHES. Nanobot's 2nd-to-last user message is NOT a rolling anchor; Aura's explicit rolling tail is a refinement justified for long agent loops (>5 turns with many tool_results).

### nanobot (anthropic_provider.py)
Status: MATCHES
The Anthropic-native path (anthropic_provider.py:378-410) adds a fourth cache point on system[-1] (the last element of a system list). But Aura's plan specifies the first system message (after the A02a split). These are semantically equivalent - both protect the static prefix.

Verdict: MATCHES SEMANTICALLY.

### hermes-agent (prompt_caching.py + run_agent.py)
Status: DEVIATES FROM PLAN (accepted)
hermes implements a dual-TTL pattern (prompt_caching.py:41-46, 59-202): markers with either "5m" (default within-session) or "1h" (prefix stability across sessions). Aura's plan A03 only declares "ephemeral" (no TTL field).

Cross-session caching is out-of-scope for Aura's Phase-KV. Aura's single-conversation cache is ephemeral. The plan is simpler and sufficient.

Verdict: YELLOW -- not over-engineering. Current plan is appropriate for scope.

### openhuman (traits.rs + compatible.rs)
Status: MATCHES (exceeds plan slightly)
openhuman's cached_input_tokens field exists. Extraction unifies OpenAI and Anthropic variants. Aura's normalization is MORE complete for multi-provider support.

Verdict: GREEN -- Aura's A04 normalization is broader, appropriate for multi-provider support.

### codex.md + cache-break diffs
Status: MATCHES
22 diffs from Claude Code all show version-string mutations (cc_version=2.1.27.cfa to cc_version=2.1.27.111). This matches Killer #1 (mutable content in the cached prefix). Aura's A02a and A02b completely address this class.

Verdict: GREEN -- plan correctly identifies and fixes the root anti-patterns.

---

## 2. Patterns Aura Is Missing

1. DeepSeek cached_tokens variant: Aura's A04 must normalize prompt_cache_hit_tokens (DeepSeek). One fallback line to add.

2. Pre-flight smoke test: A01 acceptance criteria should include explicit test against OpenRouter or native Anthropic to verify multi-system-message support before A02a lands.

---

## 3. Patterns Aura Is Over-Engineering

None identified. Plan is conservative and justified.

---

## 4. Wire-Format Bug Check (codex#2611)

Aura's plan A02b adds sort.Slice(tools[i].Function.Name) lexicographic ordering. Neither nanobot nor hermes explicitly sort tools in their cache logic (they may rely on upstream stability). Aura must add the sort because the codex bug is exactly what Aura could hit with MCP tool reordering.

Verdict: CRITICAL AND CORRECT. A02b sort fix is necessary.

---

## 5. System Message Semantics

Aura's A02a proposes two consecutive system messages (static + volatile). This is assumed but NOT verified in all reference code. Mistral or llama_server may reject.

Mitigation in plan: A01 must verify. A02a has fallback to trailing user message. Risk is mitigated.

Verdict: YELLOW -- assumption not verified in all vendors, but plan has fallback.

---

## 6. Cached_input_tokens Normalization

nanobot's priority order:
1. OpenAI/Zhipu/MiniMax/Mistral/xAI: prompt_tokens_details.cached_tokens
2. StepFun/Moonshot: cached_tokens (top-level)
3. DeepSeek/SiliconFlow: prompt_cache_hit_tokens

Aura's plan mentions Anthropic and OpenAI but NOT DeepSeek's variant. Users may point at DeepSeek via OpenRouter.

Action for A04: Add DeepSeek to fallback chain. One line of code.

Verdict: YELLOW -- completeness requires one extra fallback.

---

## Verdict Summary

| Category | Status | Notes |
|---|---|---|
| Breakpoint placement | GREEN | Semantically identical; rolling tail is justified refinement. |
| Anthropic system semantics | GREEN | First-system breakpoint matches nanobot's code. |
| Dual-TTL | YELLOW | Not in scope; single-conversation focus is clear. |
| Provider matrix | GREEN | Aura's scope appropriate. |
| cached_tokens normalization | YELLOW | Add DeepSeek variant. |
| Tool determinism | GREEN | A02b sort fix is necessary and correct. |
| Multi-system support | YELLOW | Assume but verify; plan has fallback. |
| Cache-break patterns | GREEN | Correctly identified and fixed. |
| Wire-format matrix | GREEN | Matches hermes structure. |

---

## FINAL VERDICT: GREEN with 2 YELLOW flags

GREEN signals:
- Breakpoint strategy matches production reference (nanobot).
- Cache-breaking anti-patterns correctly identified and fixed.
- Tool determinism is essential and correct.
- Observability baseline is sound.
- Plan structure (A01-A05) is proven by nanobot and hermes.

YELLOW flags (low risk, easy fixes):
1. A01 acceptance criteria: Add smoke test against OpenRouter or native Anthropic to verify multi-system-message support before A02a.
2. A04 normalization: Add prompt_cache_hit_tokens (DeepSeek) to fallback chain. One line.

NO RED FLAGS. Plan is production-ready once YELLOW items are addressed.

---

Cross-Source Confidence:
- nanobot: 9/10
- hermes: 8/10
- openhuman: 7/10
- codex + claude diffs: 10/10

Recommendation: Proceed with A01-A05 as drafted. Address YELLOW items. Plan is consistent with proven production patterns.

