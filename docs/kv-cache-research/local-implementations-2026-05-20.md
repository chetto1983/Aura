# KV-Cache / Prompt-Cache: Patterns from Curated Local Repos

Survey of D:/tmp curated reference projects, scanned 2026-05-20, for concrete prompt-cache implementation patterns Aura can lift into `internal/llm/client.go`. Aura's situation: OpenAI-compatible HTTP client, stable prefix of ~5-25 K tokens (system prompt overlays + tool definitions), wants ~3x input-cost reduction and lower latency. Three repos shipped production-grade patterns (nanobot, hermes-agent, openhuman), one shipped a high-signal anti-pattern catalog (claude/, plus the codex.md OpenAI engineering post). The other targets (picobot, cli-printing-press, mem0) had nothing relevant.

## nanobot (Python) — clean reference architecture

Two-provider split with a `ProviderSpec` capability flag and shared marker placement logic. Closest fit to Aura's shape (Aura already has provider-agnostic LLM client, would just add a `SupportsCacheControl` bool).

**Capability flag in registry** — `D:/tmp/nanobot/nanobot/providers/registry.py:64`:
```python
# Provider supports cache_control on content blocks (e.g. Anthropic prompt caching)
supports_prompt_caching: bool = False
```
Set `True` only on `anthropic` (native) and `openrouter` (gateway). Local-server providers (vllm/ollama/lm_studio) and OpenAI default stay `False` — OpenAI does prefix-caching automatically, no markers needed.

**Marker application — OpenAI-wire** — `D:/tmp/nanobot/nanobot/providers/openai_compat_provider.py:356-388`:
```python
@classmethod
def _apply_cache_control(cls, messages, tools):
    """Inject cache_control markers for prompt caching."""
    cache_marker = {"type": "ephemeral"}
    new_messages = list(messages)
    def _mark(msg):
        content = msg.get("content")
        if isinstance(content, str):
            return {**msg, "content": [{"type": "text", "text": content,
                                         "cache_control": cache_marker}]}
        if isinstance(content, list) and content:
            nc = list(content); nc[-1] = {**nc[-1], "cache_control": cache_marker}
            return {**msg, "content": nc}
        return msg
    if new_messages and new_messages[0].get("role") == "system":
        new_messages[0] = _mark(new_messages[0])
    if len(new_messages) >= 3:
        new_messages[-2] = _mark(new_messages[-2])
    ...
```
The 4-breakpoint budget gets spent as: **system + last-tool + builtin/MCP boundary tool + 2nd-to-last user message**. The "MCP boundary" trick is in `base.py:235`:
```python
@classmethod
def _tool_cache_marker_indices(cls, tools):
    """Return cache marker indices: builtin/MCP boundary and tail index."""
    tail_idx = len(tools) - 1
    last_builtin_idx = None
    for i in range(tail_idx, -1, -1):
        if not cls._tool_name(tools[i]).startswith("mcp_"):
            last_builtin_idx = i; break
    ...
```
Brilliant: MCP tools mutate per-session (servers come and go), so put a breakpoint at the *boundary* — built-in tools below cache forever, the MCP-tools section above caches but invalidates more cheaply.

**Anthropic-side `_apply_cache_control`** — `D:/tmp/nanobot/nanobot/providers/anthropic_provider.py:378-410`. Adds a fourth cache point on the system message itself (`system[-1]['cache_control'] = marker`) which is illegal on OpenAI-wire but required on Anthropic.

**Usage extraction across N providers** — `openai_compat_provider.py:782-829` normalizes 3 different provider field names down to a single `cached_tokens` key:
```python
for path in (
    ("prompt_tokens_details", "cached_tokens"),  # OpenAI/Zhipu/MiniMax/Qwen/Mistral/xAI
    ("cached_tokens",),                          # StepFun/Moonshot (top-level)
    ("prompt_cache_hit_tokens",),                # DeepSeek/SiliconFlow
):
    cached = cls._get_nested_int(usage_map, path)
    ...
    if cached: result["cached_tokens"] = cached; break
```
Plus Anthropic-side at `anthropic_provider.py:509-526` maps `cache_creation_input_tokens` + `cache_read_input_tokens` and exposes them separately while normalizing read to `cached_tokens`.

**Test discipline** — `tests/providers/test_cached_tokens.py` has 8 cases covering each provider's wire format (dict + SDK object); `tests/agent/test_context_prompt_cache.py:90-105` explicitly asserts "user content must precede runtime context for prefix stability" (the famous mistake of injecting a timestamp at the *start* of the user message would force a per-turn cache miss).

**How Aura should lift this**: copy the structure 1:1. Add `SupportsCacheControl bool` to Aura's provider config (probably in `internal/llm/client.go` config struct or settings catalog), wrap message construction in a Go equivalent of `_apply_cache_control` that takes `[]Message + []Tool` and returns the same with `cache_control` markers on system + last-N user msgs + the builtin/MCP boundary in tools. Then in `Stream()` response handling, extract `usage.prompt_tokens_details.cached_tokens` (OpenAI/Mistral/xAI) and emit it as a structured log + metric.

## hermes-agent (Python) — capability matrix gold + dual-TTL strategy

The most mature provider-detection logic in the survey. Worth lifting wholesale if Aura ever supports more than 1-2 LLM endpoints.

**Provider × wire-format decision matrix** — `D:/tmp/hermes-agent/run_agent.py:3412-3505`, function `_anthropic_prompt_cache_policy()` returns `(should_cache, use_native_layout)`. The native-vs-envelope distinction is the key insight: cache_control on inner content blocks (native Anthropic, MiniMax-anthropic) vs on the message envelope (OpenRouter, Qwen-on-OpenCode):
```python
if is_native_anthropic: return True, True
if (is_openrouter or is_nous_portal) and is_claude: return True, False
if is_anthropic_wire and is_claude: return True, True  # 3rd-party gateway
# MiniMax explicit allowlist by host (provider name unreliable across configs)
if is_anthropic_wire and (is_minimax_provider or is_minimax_host): return True, True
# Qwen on OpenCode-Go/Alibaba: OpenAI-wire but rewards cache_control markers
if provider_is_alibaba_family and model_is_qwen: return True, False
return False, False  # default: stay off, don't ship markers to strict providers
```
With a comment-anti-pattern explicitly called out: `test_custom_openai_wire_does_not_cache_even_with_claude_name` (line 167 of the test file) — "sending cache_control fields in OpenAI-wire JSON can trip strict providers that reject unknown keys. Stay off unless the transport is explicitly anthropic_messages or the aggregator is OpenRouter." This is THE gotcha for Aura — Aura currently talks OpenAI-wire to whatever endpoint, so blanket-adding cache_control could 400 a vanilla Mistral or local llama-server.

**Dual-TTL strategy** — `D:/tmp/hermes-agent/agent/prompt_caching.py:59-202`. Two layouts:
- `system_and_3`: 4 breakpoints all at 5-minute TTL (default within-session).
- `prefix_and_2`: 1h TTL on system prefix + tools tail + 5m TTL on last 2 messages. Used cross-session for Claude on native Anthropic / OpenRouter / Nous Portal:
```python
def _build_marker(ttl):
    marker = {"type": "ephemeral"}
    if ttl == "1h":
        marker["ttl"] = "1h"
    return marker
```
And critically `mark_tools_for_long_lived_cache()` marks `tools[-1]` (line 197) — "Anthropic prefix-cache order is `tools → system → messages`. Marking the last tool dict caches the entire tools array". On OpenRouter this passes through as-is.

**Stability discipline** — `D:/tmp/hermes-agent/website/docs/developer-guide/prompt-assembly.md` documents 10 layers of the system prompt explicitly ordered for stability: identity → tool guidance → memory snapshot → user profile snapshot → skills index → context files → timestamp **LAST**. The doc says: "Memory snapshots ... are injected as frozen snapshots at session start. Mid-session writes update disk state but do not mutate the already-built system prompt until a new session or forced rebuild occurs." Aura's `SOUL.md`/`AGENT.md`/`USER.md`/`TOOLS.md` overlays should be loaded ONCE per conversation, not per-turn — currently CLAUDE.md says they are read "every turn from PROMPT_OVERLAY_PATH" which is fine for hot-edit ergonomics but means a file mtime change mid-session blows the cache.

**How Aura should lift this**: skip the whole multi-provider matrix for now (Aura has effectively one LLM endpoint). But adopt the *decision contract* (`should_cache, use_native_layout`) as a config struct so when Aura's user points at OpenRouter/Anthropic later, the gates are already in place. And take the dual-TTL idea: if Aura's prefix is stable across hours (overlay files unchanged), explicit `ttl: "1h"` cuts the prefix re-build cost.

## openhuman (Rust) — usage observability done right

Doesn't ship cache markers itself (its primary backend handles caching server-side) but the `Usage` struct shape is the cleanest abstraction in the survey:

`D:/tmp/openhuman/src/openhuman/inference/provider/traits.rs:73-77`:
```rust
/// Number of input tokens that were served from the KV cache
/// (returned by backends that support prompt caching, e.g. via
/// `openhuman.usage.cached_input_tokens` or
/// `prompt_tokens_details.cached_tokens`).
pub cached_input_tokens: u64,
```
And `compatible.rs:686-690` does the unification:
```rust
let cached_input_tokens = oh_usage
    .and_then(|u| u.cached_input_tokens)
    .or(std_usage.and_then(|u| u.prompt_tokens_details.as_ref())
                 .map(|d| d.cached_tokens))
    .unwrap_or(0);
```

**How Aura should lift this**: add a `CachedInputTokens uint64` field to whatever Aura's per-turn usage struct is in `internal/llm`, populate it in the response parser by reading `usage.prompt_tokens_details.cached_tokens`, and emit it in the structured log line + the `/api/conversations` archive so the cache hit rate is grepable post-hoc. Without observability the optimization is invisible.

## D:/tmp/claude/ + codex.md — anti-pattern catalog

The 22 `cache-break-*.diff` files in `D:/tmp/claude/` are *Claude Code's own* observed cache-invalidation events. Every single one shows the same two breakage classes:

1. **Version string in system prompt header**:
```
-x-anthropic-billing-header: cc_version=2.1.41.0f7; cc_entrypoint=cli; cch=00000;
+x-anthropic-billing-header: cc_version=2.1.41.8cc; cc_entrypoint=cli; cch=00000;
```
Every Claude Code patch release writes a new version into the system prompt → cache miss on first call after upgrade.

2. **Recent git commits embedded near top of prompt**:
```
-3b402d0 docs: update STATE.md for v1.2 roadmap ready
+6f1a902 docs(planning): remove outdated deferred milestone notes
```
Every new commit in the repo invalidates the cache. Anything time-or-git-state-correlated injected into the cacheable prefix kills the hit rate.

**codex.md (OpenAI engineering blog)** — the canonical anti-pattern list at line 691-705:
> *Cache hits are only possible for exact prefix matches within a prompt. To realize caching benefits, place static content like instructions and examples at the beginning of your prompt, and put variable content, such as user-specific information, at the end. This also applies to images and tools, which must be identical between requests.*
>
> Operations that cause a cache miss in Codex:
> - Changing the `tools` available to the model in the middle of the conversation.
> - Changing the `model` that is the target of the request.
> - Changing the sandbox configuration, approval mode, or current working directory.

And the engineering discipline they apply (line 701-704):
> *When possible, we handle configuration changes that happen mid-conversation by appending a new message to `input` to reflect the change rather than modifying an earlier message.*

Then a specific bug they shipped + fixed (line 699): MCP tool enumeration order was non-deterministic — every turn produced a different tools-array byte sequence → 100% cache miss. PR linked: codex#2611.

## Patterns NOT worth lifting

- **Hermes' per-host hardcoded allowlist** (`api.minimax.io`, `api.minimaxi.com`, `opencode.ai`, etc.): valuable in their multi-provider gateway world; in Aura's single-endpoint world it's noise. Build the decision-contract scaffold but populate only `anthropic`/`openai`/`openrouter` slots.
- **Anthropic SDK direct integration** (nanobot's `AnthropicProvider`): Aura's contract is OpenAI-compatible HTTP only. Stay there. If a user wires Aura at native Anthropic later, the `cache_control` markers ride through the OpenAI-wire JSON just fine (Anthropic accepts both formats; OpenRouter passes them through).
- **The `system_and_3` 4-breakpoint maximalism without a budget check**: Anthropic charges per breakpoint above the prefix point — 4 markers means up to 4 cache regions to maintain. Aura should start with 2 (system + last user msg) and measure before adding more.
- **mem0's `cache_control` references**: those are in `openclaw/` which is an unrelated security-isolation library, not memory-related. False positive in the grep — confirmed by reading the matched files. Skip.
- **picobot/cli-printing-press**: no relevant caching code, only one stray "ephemeral session" comment in picobot's loop.go that's about clearing history per-channel, not prompt caching.

## Concrete Aura adaptation

Given Aura's `internal/llm/client.go` is OpenAI-compatible HTTP-only and the chat LLM endpoint is currently configured per-install (could be vLLM/Mistral/OpenAI/anthropic-via-openrouter), the minimum-LOC implementation is **the nanobot pattern, scoped to OpenAI-wire only**:

1. **Add capability flag** to whatever config struct holds LLM settings (probably `internal/config` or the settings catalog). Default `false`. Set `true` only when the operator points Aura at OpenRouter or a known Anthropic-compatible endpoint. Could be inferred from base URL host (`openrouter.ai`, `api.anthropic.com`, `api.minimax.io/anthropic`) to make it zero-config for those cases.

2. **OpenAI's automatic prefix caching is free** for direct `api.openai.com` requests — no markers needed, no flag needed. The current Aura code path already gets this if the operator points at OpenAI. Just make sure the system prompt is *stable*: no timestamps, no git-state, no random session IDs in the cacheable prefix.

3. **Add a `applyCacheControl([]Message, []Tool) ([]Message, []Tool)` helper** that, gated on the capability flag:
   - Wraps the system message's string content in `[{"type":"text","text":..., "cache_control":{"type":"ephemeral"}}]`.
   - Marks the last tool in the tools array with `cache_control: {"type":"ephemeral"}`.
   - Optionally marks the second-to-last user message (only if conversation length ≥ 3).
   Implementation is ~50 LOC in Go, easily a single file.

4. **Stability discipline** (this is the big one — applies even without #1-#3 because OpenAI auto-caches):
   - Move the **current timestamp injection** (if any) to the *end* of the user turn, not into the system prompt or the start of the user message.
   - Lock the overlay files (`SOUL.md`/`AGENT.md`/`USER.md`/`TOOLS.md`) to read-once-per-conversation, not per-turn. If hot-reload is needed, gate it behind an explicit signal (file checksum + 5-min throttle).
   - Sort MCP tools deterministically before emitting (alphabetically by name) so MCP server re-registration doesn't reshuffle the tools array — this is the codex#2611 bug, would be very easy to hit in Aura.
   - When the agent loop adds info mid-conversation (cwd change, sandbox mode change, settings update), append a NEW user/system message at the tail rather than rewriting an earlier message. See codex.md line 701-704.

5. **Observability**: extract `usage.prompt_tokens_details.cached_tokens` from every response (it's in the OpenAI-format usage block already, free), log it via zap, and store it in the conversation archive table next to `prompt_tokens`. Build a `/api/cache_stats` endpoint or a dashboard widget that shows rolling hit rate. Without this you cannot tell if any of the above is working.

Total LOC: capability flag + helper + parser change ≈ 80-120 LOC. Discipline changes (overlay caching, tool ordering, timestamp placement) are surgical edits across 3-5 existing files. The observability piece (~30 LOC) is the most important — ship it first, measure baseline hit rate (probably 0% on OpenRouter without markers, possibly already 30-60% on direct OpenAI), then add markers and measure delta.
