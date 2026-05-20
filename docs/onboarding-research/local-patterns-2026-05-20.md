# LLM-driven self-onboarding — patterns from D:/tmp curated repos

**Date:** 2026-05-20
**Scope:** scan of curated local repos for prior art on an agent interviewing the user at first contact and self-composing persona / runtime / user overlay files. Sibling research thread is doing the wider web; this doc only covers what already sits on disk.

**TL;DR:** three repos have first-class material — `openhuman` ships a **complete welcome-agent pattern** (agent.toml + prompt.md + paired tools), `nanobot` ships a TTY wizard that walks a Pydantic schema (mostly UI, but the templates/SOUL.md + templates/USER.md scaffold is gold), and `picobot` ships the file-scaffold half (writes SOUL.md / USER.md / AGENT.md but **does not interview**). `mem0` contributes the fact-extraction prompt pattern that's the missing link between conversation transcript and structured overlay file. Hermes adds a Telegram-pinned "intro message" UX idiom.

---

## openhuman — `D:/tmp/openhuman`

Closest match in the corpus. A **dedicated `welcome` agent** is selected as the first agent for any new user; its only job is interview + at-least-one-app-connected + finalize. The pattern is:

```
welcome/agent.toml         declarative agent registration (id, tools, temp)
welcome/prompt.md          ~150-line system prompt = the entire flow
welcome/prompt.rs          context renderer (PROFILE.md + connected apps)
tools/impl/agent/
  check_onboarding_status  read-only snapshot tool the agent calls EVERY turn
  complete_onboarding      finalizer that flips chat_onboarding_completed
  onboarding_status        shared compute_state() helper, criteria gate
```

### Trigger condition

`src/openhuman/tools/impl/agent/onboarding_status.rs:321-327`

```rust
let onboarding_status = if !is_authenticated {
    "unauthenticated"
} else if config.chat_onboarding_completed {
    "already_complete"
} else {
    "pending"
};
```

A single boolean `config.chat_onboarding_completed` is the latch. Channel router picks the `welcome` agent over the normal orchestrator when it's `false`. **Takeaway for Aura:** mirror this — Aura already has `cfg.IsBootstrapped()` for *credentials* bootstrap; she needs a separate `chat_onboarding_completed` (overlay-files bootstrap) latch in SQLite settings.

### The "always call status first" pattern

`src/openhuman/agent/agents/welcome/prompt.md:7`

```
1. **ALWAYS call `check_onboarding_status` as your first action on every turn.** No exceptions. Call it before generating any visible text. You need the snapshot to know what's connected.
```

The prompt forces a structured **environment probe tool call at the start of every turn**. The tool returns a compact markdown snapshot (≤300 bytes — see `format_status_markdown` lines 198-268), not JSON, because tokens. **Takeaway:** Aura's onboarding system prompt should bake in "first action = `wiki_path` or a new `onboarding_status` shim that lists which of SOUL/AGENT/USER.md are present and non-empty". One read-only tool call per turn, deterministic ground truth.

### The state-tracking pattern is NOT a state machine

The prompt does NOT enumerate "turn 1 = name, turn 2 = role, turn 3 = …". It enumerates a **discovery phase + opportunism rule**:

`src/openhuman/agent/agents/welcome/prompt.md:17-40`

```
Before you touch the setup checklist, spend a couple of turns learning about the user. Casual tone, no interrogation.
Turn order:
1. First turn (the opener): greet warmly, ask what brought them here…
2. Second turn: ask about their daily tools…
3. Third turn (only if needed): ask what's annoying…
Be opportunistic — act on what they say immediately…
One question per turn.
```

This is **soft sequencing** (LLM judgment with hard rails), not a code-side state machine. The hard rails are:
- one question per turn (rule 7);
- ≤3 sentences per message (rule 7);
- no markdown formatting because the chat surface is plain text (rule 3);
- finalize when "ready_to_complete && farewell signal detected" (rule 6).

**Takeaway:** Aura should resist building a Go state machine. The model is the state. The code just enforces the latch and provides the snapshot tool.

### The finalizer rejects premature calls

`src/openhuman/tools/impl/agent/complete_onboarding.rs:107-111`

```rust
if !state.ready_to_complete {
    return Ok(ToolResult::error(build_not_ready_to_complete_error(
        &state.ready_to_complete_reason,
    )));
}
```

The agent can *try* to call `complete_onboarding` early, and the tool rejects with a human-readable error explaining what's still missing. The agent reads the error and keeps conversing. This is the **server-side guard** that means a hallucinated finalize never escapes. **Takeaway:** Aura's `finalize_onboarding` tool MUST re-check the criteria server-side (overlays non-empty, name set, language set) and reject with a reason string. Cheaper than fighting prompt drift.

---

## nanobot — `D:/tmp/nanobot`

Two pieces of relevance:

### 1. SOUL.md / USER.md TEMPLATES (exact convention match)

`nanobot/templates/SOUL.md` and `nanobot/templates/USER.md` use the **same names Aura uses**. The USER.md template (lines 1-49) is a fillable checkbox form:

```markdown
# User Profile
## Basic Information
- **Name**: (your name)
- **Timezone**: (your timezone, e.g., UTC+8)
- **Language**: (preferred language)
## Preferences
### Communication Style
- [ ] Casual
- [ ] Professional
- [ ] Technical
### Response Length
- [ ] Brief and concise
…
```

**Takeaway for Aura:** start with this checkbox-form scaffold as the *target* shape of USER.md the onboarding agent fills. Translates a free-form chat into structured persistent state without forcing the agent to invent schema. Aura already has a SOUL/AGENT/USER convention — port the nanobot scaffold verbatim as the initial blank to overwrite.

### 2. `nanobot/cli/onboard.py` — terminal questionnaire (mostly NOT relevant)

1170-line interactive TTY wizard using `questionary` that walks the Pydantic `Config` schema. **This is the wrong shape for Aura.** It's a CLI menu, not a chat. It does prove that Pydantic-schema-driven introspection (`_get_field_type_info`, `_get_field_display_name` lines 178-228) collapses a config tree into a generic form. **Lift one thing only:** the `_summarize_model` (lines 987-1003) recursive summary — Aura's confirmation-edit loop can use the same pattern to show the user "here's what I drafted, edit?" before committing the files.

### 3. `tests/agent/test_onboard_logic.py:_merge_missing_defaults`

`tests/agent/test_onboard_logic.py:30-100` exercises a recursive `_merge_missing_defaults(existing, defaults)` helper that backfills missing nested keys without overwriting user customizations. **Takeaway:** the same merge primitive lets Aura's onboarding re-run idempotently — if SOUL.md is already half-written, only add the missing sections.

---

## picobot — `D:/tmp/picobot`

The *non-LLM* version of the same idea. `picobot.Onboard()` (`internal/config/onboard.go:377-392`) writes hardcoded SOUL.md / AGENT.md / USER.md / TOOLS.md / HEARTBEAT.md / MEMORY.md scaffolds to `~/.picobot/workspace/` on first launch:

```go
func Onboard() (string, string, error) {
    cfgPath, workspacePath, err := ResolveDefaultPaths()
    ...
    cfg := DefaultConfig()
    cfg.Agents.Defaults.Workspace = workspacePath
    if err := SaveConfig(cfg, cfgPath); err != nil { ... }
    if err := InitializeWorkspace(workspacePath); err != nil { ... }
    return cfgPath, workspacePath, nil
}
```

Inside `InitializeWorkspace` (lines 55-332) the files are **literal-string templates** identical in shape to nanobot's. The USER.md has an explicit `**Name**: (your name)` slot for the user to edit manually.

**Crucial gap picobot does NOT fill:** there is no interview, no LLM, no chat flow. The user is expected to open the file in an editor and fill it in by hand. **This is exactly the pain Aura wants to eliminate** — Aura already has `ask_user` and `workspace_write`, so she can *do the interview and the edit* without forcing the user out of Telegram.

**Takeaway:** picobot defines the *target artifacts*; openhuman defines *how an agent fills them via chat*. Aura wants picobot's output produced by openhuman's flow.

---

## mem0 — `D:/tmp/mem0`

`mem0/configs/prompts.py:15-60` — `FACT_RETRIEVAL_PROMPT`. This is the **transcript → structured-facts extractor** Aura needs as the second half of onboarding:

```
You are a Personal Information Organizer, specialized in accurately storing facts, user memories, and preferences. Your primary role is to extract relevant pieces of information from conversations and organize them into distinct, manageable facts.
Types of Information to Remember:
1. Store Personal Preferences…
2. Maintain Important Personal Details…
…
Input: Hi, my name is John. I am a software engineer.
Output: {"facts" : ["Name is John", "Is a Software engineer"]}
```

It's a one-shot LLM call: takes a conversation, returns a JSON list of facts. **Takeaway for Aura:** after the 5-10 interview turns, Aura can fire this exact pattern once (`temperature=0`, JSON-mode) on the transcript to *extract* the facts, then a second deterministic call to *format* them into USER.md sections. Or skip the intermediate JSON and have the same agent that ran the interview write the files directly via `workspace_write`. The mem0 pattern is the proof that "conversation → structured fact list" is a single LLM call.

---

## hermes-agent — `D:/tmp/hermes-agent`

Only a UX idiom worth lifting. `docs/plans/2026-05-02-telegram-dm-user-managed-multisession-topics.md:54-71` describes a `/topic`-triggered Telegram flow that:

1. Sends an onboarding message;
2. **Pins the onboarding message** in the chat (so the user can re-read the orientation without scrolling);
3. Lists restorable old sessions.

**Takeaway:** when Aura finalizes onboarding, send a Telegram pin with a "you're set up, here's what I know about you" recap — this gives the user a durable artifact in the chat itself, mirroring picobot's editable files. Aura already has Telegram pin capability via `bot.PinChatMessage` (already used elsewhere in `internal/channels/telegram`).

---

## Closest analog in the wild

**`openhuman`'s welcome agent (`src/openhuman/agent/agents/welcome/`) + the paired tools (`check_onboarding_status`, `complete_onboarding`).**

It is exactly the pattern the user described:
- single boolean latch (`chat_onboarding_completed`) flips the channel router to a special agent;
- a ~150-line markdown system prompt does ALL the orchestration — no Go state machine;
- one read-only "status snapshot" tool call per turn = environment ground truth;
- one server-guarded finalizer tool that rejects premature calls with a reason string;
- soft turn-order in the prompt (opener → daily tools → annoyance) with opportunism rule + escape hatch;
- conversation surface = the product itself (Telegram for Aura, in-app chat for openhuman) — the user *experiences* the agent during the interview, no separate wizard UI.

The single concept Aura needs to copy 1:1 is the **"first action every turn is a snapshot tool call"** rule, which keeps the model grounded in actual filesystem state instead of hallucinating which files are written. The single concept Aura should NOT copy is openhuman's `<openhuman-link>` pill convention — Telegram has no equivalent, plain prose suffices.

---

## Concrete Aura onboarding sketch

### (a) Trigger condition

Add `chat_onboarding_completed BOOLEAN DEFAULT FALSE` to SQLite settings (separate from `IsBootstrapped()`, which is purely credential-level). The agent loop's existing prompt-overlay loader (`PROMPT_OVERLAY_PATH` → SOUL.md/AGENT.md/USER.md/TOOLS.md) already short-circuits empty files. Insert one check at the top of `internal/chat/agentloop.go`:

```
if !cfg.ChatOnboardingCompleted && allOverlaysEmptyOrMissing(SOUL, AGENT, USER) {
    systemPrompt = onboarding.Prompt(ctx)  // override the normal builder
}
```

No new agent class, no router fork — just a system-prompt swap. Cheaper than openhuman's full second agent because Aura already has *one* loop and *one* tool registry; we just hand it a different system prompt and let it use the tools it already has.

### (b) ~10-line system-prompt overlay (drop-in)

```
You are meeting this user for the first time. Their SOUL.md, AGENT.md and USER.md overlays are blank. Your single job this conversation is to interview them and fill those files. ONE question per turn. Plain Telegram-friendly prose, no markdown formatting in your replies, ≤3 sentences. Open by greeting in their language (mirror whatever language they wrote in) and asking the most useful question first: their name and what they want this assistant to be for them. Then progressively learn (in any order driven by their answers): preferred language, timezone, communication style (casual/professional/technical), main topics/projects, and any hard preferences (e.g. "always answer in Italian", "never use bullet lists"). When you have enough — minimum: name + language + one concrete use-case — call `workspace_write` THREE times to write USER.md, SOUL.md, AGENT.md based on what you learned. Then call `finalize_onboarding`. If the user says "skip" or "just set me up", write minimal files with what you have and finalize. Never invent facts. If a field is unknown, leave the section empty; don't make up a hobby.
```

### (c) State machine (turns 1-N) — soft, in prompt, not in Go

| Turn | LLM action (driven by prompt above) | Tools fired |
|------|-------------------------------------|-------------|
| 1 | Open + ask name + purpose | none — pure text |
| 2 | Echo back what was heard, ask preferred language + timezone | none |
| 3 | Ask communication style + main topics | none (or `web_search` if user names a niche topic to confirm) |
| 4 | Ask any non-negotiable preferences ("anything that would make this useless?") | none |
| 5 | Recap as plain prose, ask "shall I commit this to your profile?" | none |
| 6 | On user confirm → write three files | `workspace_write` × 3 (USER.md, SOUL.md, AGENT.md) in parallel — independent tool calls, the loop already parallelizes |
| 7 | `finalize_onboarding` → server-side criteria gate → flip flag → send Telegram pin recap | `finalize_onboarding` |

No `ask_user` tool needed — the chat IS the question. `ask_user` is overhead for a flow that's already a chat. Reserve `ask_user` for non-conversational contexts (cron-triggered agents that need user input).

### (d) Exit condition + commit step

Implement one new tool: `finalize_onboarding` in `internal/tools/`. It:

1. Reads SOUL.md, USER.md, AGENT.md from `WIKI_PATH` (or `PROMPT_OVERLAY_PATH`);
2. Validates: each file is ≥ 50 bytes AND USER.md contains a non-placeholder `Name:` field AND a `Language:` field (regex check on the structured frontmatter or a simple substring scan);
3. If fail → returns `ToolResult.error("Cannot finalize: USER.md is missing a name. Please ask the user.")` — agent reads, asks again;
4. If pass → updates SQLite `chat_onboarding_completed = TRUE`, sends a Telegram message + pins it: "Profile saved. Here's what I know: …" (recap built from the filled fields);
5. Next turn, the agent-loop's normal prompt builder takes over (loads the just-written overlays as context).

**Verdict: single-prompt mode wins, NOT a state machine.** Aura's existing agent loop + tool registry are sufficient. The whole onboarding feature reduces to:

1. One SQLite column;
2. One ~10-line system-prompt overlay swap;
3. One new server-guarded `finalize_onboarding` tool;
4. Borrow nanobot's USER.md scaffold as the structural target;
5. Borrow mem0's "extract facts from conversation" pattern *only if* you find the agent struggling to write directly (probably won't — modern models compose fine).

Total: ~200 LOC + one prompt file. The lever-arm beats picobot (file-only, no chat) and nanobot (CLI wizard, wrong surface) because Aura already has the chat surface, the tools, the file convention, and the LLM loop — onboarding is the smallest possible glue, not a new subsystem.
