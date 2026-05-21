# cli-printing-press — Output Discipline Patterns

**Date:** 2026-05-21
**Source:** `D:/tmp/cli-printing-press` (v4, Wave B policy)
**Trigger:** Aura's "Trova un cliente e stampalo" turn dumped all 90 rows from `execute_code` (36s, no synthesis).

## TL;DR

Printing-press never lets raw tool/command output reach the agent's reply: every captured stdout has a hard 4 KiB cap **plus** a generator-injected client-side truncation (`truncateJSONArray(data, --limit)`), and a forked `output-review` sub-agent judges the truncated sample with a 50-word-per-finding budget. The architecture is **substrate-level, not prompt-level** — the runtime physically can't emit the full payload. Aura is the inverse: `execute_code` returns the entire 90-row table verbatim and only prompt etiquette stops a dump.

## Patterns

### P1 — Substrate-level output cap, not prompt rule

**File:** `internal/pipeline/live_check.go:103`, `:451-459`
**Rule:** `const outputSampleMaxBytes = 4096`, applied via `sampleOutput(s)` to every captured stdout before persistence; truncation marker `"…[truncated]"` is appended so downstream readers know the payload was cut.
**Why it fixes Aura:** Tool results currently round-trip with no upper bound — `execute_code` returns the 90-row table into both LLM context and reply. Inject a 4 KiB (or 2 KiB) cap on every tool body at the `internal/tools/*` `Execute` return path, append `"…[N more rows truncated]"`, and the dump becomes physically impossible. **Biggest lift.**

### P2 — Client-side `--limit` truncation injected at code-gen time

**File:** `internal/generator/generator.go:3264-3296` (`endpointNeedsClientLimit`) + `internal/generator/templates/command_endpoint.go.tmpl:579-582` (`data = truncateJSONArray(data, flagLimit)`)
**Rule:** Any GET list endpoint with a `limit` param + no declared pagination block gets a **post-API client-side truncation** injected into the generated command. Rationale comment (`:3273-3278`): "APIs like Firebase and various file-backed JSON endpoints accept the query param without applying it server-side; truncation is harmless when the API DID return only N items already (idempotent)."
**Why it fixes Aura:** `execute_code` (and `list_*`) can't trust LLM-supplied `limit` — sandboxed Python doesn't enforce it. Wrap any tool that returns array-of-records with `truncateRows(data, defaultLimit=10)` and make the LLM explicitly ask for more. The "harmless when short, life-saving when long" rule applies 1:1. (Battle-tested via their retro #350 F6.)

### P3 — Separate review agent runs on the *truncated sample*, never the raw output

**File:** `skills/printing-press-output-review/SKILL.md:64-80`
**Rule:** The reviewer agent only ever sees `output_sample` (the 4 KiB-capped slice in `live_check.features[]`), never the original stdout. The prompt contract says: "actual stdout (in `output_sample`, bounded to ~4 KiB)" — explicit confirmation that no path exists to feed full output back. Findings are capped at **50 words each** (line 73: "report findings under 50 words each").
**Why it fixes Aura:** Aura's loop chains tool→LLM→tool→LLM and each step re-reads the previous tool's full output. A "summarize this" sub-step that always sees only a truncated sample + "your output must be under N words" kills the dump impulse. This is Aura's missing **synthesis stage**.

### P4 — `context: fork` isolation for synthesis sub-agents

**File:** `skills/printing-press-output-review/SKILL.md:10` (`context: fork`) + `:21` ("the reviewer agent's diagnostic chatter stays isolated from the calling skill's context")
**Rule:** The review/synthesis stage runs in a forked context so its working notes/intermediate reasoning never pollute the parent agent's reply. Parent only consumes the structured `---OUTPUT-REVIEW-RESULT---` block (lines 88-117).
**Why it fixes Aura:** Aura is single-context — every tool result bloats the SAME context producing the reply, so the LLM "knows everything" and renders everything. A forked `synthesize_for_user(tool_result) -> str` with `temperature=0` + strict schema turns "dump 90 rows" into "1 customer + 1 sentence". Precedent: `internal/wiki` already uses `temperature=0` deterministic writes.

### P5 — Structured result block as the only emitter

**File:** `skills/printing-press-output-review/SKILL.md:82-117`
**Rule:** The sub-agent's reply MUST end with `---OUTPUT-REVIEW-RESULT---` ... `---END-OUTPUT-REVIEW-RESULT---` with a fixed schema (`status`, `findings[]`, each finding `check/severity/description/suggestion`). The parent parses ONLY this block. Free-text outside is discarded.
**Why it fixes Aura:** Today Aura's tool result is unstructured text the model can freely paraphrase or echo. Enforce a **structured intermediate** between tool and reply: `{summary: str (<= 280 chars), highlights: [{row_id, fields}], total_seen: int}`. The agent's final reply layer renders from the struct; the raw payload is never in the rendering scope.

### P6 — Description-shaping at registration time (mcpdesc.Compose)

**File:** `internal/mcpdesc/compose.go:28` (`optionalListMax = 3`), `:141-153` (`Optional: a, b, c (plus N more).`), `:34` (`defaultValueMaxLen = 30`)
**Rule:** Tool descriptions the LLM sees are **themselves bounded**: max 3 optional params listed inline before collapsing to "(plus N more)"; defaults longer than 30 chars are dropped entirely. The agent is taught — by the description it reads at boot — that "more exists, ask for it" is the default modality.
**Why it fixes Aura:** Bias the model toward synthesis by example. Re-write `execute_code`'s and `list_*` tool descriptions to end with the literal phrase "Returns up to N items + a count of remaining; ask explicitly for more." The LLM mirrors the verbs in its tool descriptions; if every list-tool description models "show some + offer more", the reply style follows.

### P7 — Method-marker injection ("Destructive.", "Partial update.")

**File:** `internal/mcpdesc/compose.go:228-244` (`appendMethodMarker`)
**Rule:** Every DELETE tool description has `"Destructive."` appended unconditionally; PATCH gets `"Partial update."`. These aren't author opinions — they're **substrate-injected behavior hints** the LLM cannot opt out of. Single source: line 234-236.
**Why it fixes Aura:** Inject a `"Summarize, do not dump."` marker on every read/list/exec tool description at the registry layer (`internal/tools/registry.go`). The LLM sees this hint on every turn without prompt-overlay edits. Cheap, central, ungameable.

### P8 — AI-slop regex catches dumps after the fact

**File:** `internal/generator/textfilter.go:18-27` (`aiSlopPatterns`)
**Rule:** Generated README/help text is scanned for slop markers (`comprehensive`, `seamless`, `leverage`, "in today's X landscape"); warnings emitted, not blocked. A signal-not-gate that catches drift over time.
**Why it fixes Aura:** Add a post-reply regex/length check in `internal/channels/telegram/outbound.go` — if reply > 4 KiB **and** contains > 10 newlines **and** there's no preceding "user asked for full data" marker, log a `output_discipline_drift` event and (optionally) prepend "Here are the first 10 — say 'more' for the rest." Drift catcher, not blocker. Pairs with the quality-snapshot.md living-doc pattern from Aura's own behavioral rules.

## Cross-ref with prior cli-printing-press study (2026-05-15)

Memory `project_cli_printing_press_eval_2026-05-15`:

- **`mcpdesc.Compose` (P6, P7):** previously studied for *parameter discipline*; **the output-discipline angle (method markers as behavioral hints) was NOT lifted** — open follow-up.
- **`references/` folder pattern:** ADOPTED as `internal/skills` reference structure.
- **State-machine ortogonale:** ADOPTED for agent-loop steps; the new `context: fork` finding (P4) is a different axis (context isolation, not state). Worth adopting separately.
- **NOT adopted:** full printing-press substrate. Output-discipline lifts above are extractable.

## Recommended Aura plan (priority order)

1. **P1 + P2** — wrap every tool's `Execute()` with `boundOutput(maxBytes=4096, maxRows=10)` at the registry boundary. Single-commit in `internal/tools/registry.go`. Highest ROI, lowest blast radius.
2. **P5** — define `ToolResultSummary` struct, refactor `execute_code` first, then `list_*` / `search_*`.
3. **P7** — inject "Summarize, do not dump." marker on read-shaped tool descriptions at the registry layer. Free.
4. **P3 + P4** — forked synthesis sub-agent (`temperature=0`, structured output) above threshold. Larger; defer until 1-3 land.
5. **P8** — drift catcher as observability only.

## Hard refs (file:line)

- `internal/pipeline/live_check.go:103,451-459` — `outputSampleMaxBytes`, `sampleOutput()`
- `internal/generator/generator.go:3264-3296` — `endpointNeedsClientLimit`
- `internal/generator/templates/command_endpoint.go.tmpl:579-582` — injection site for `truncateJSONArray`
- `internal/generator/limit_truncation_test.go:14-60` — test matrix for the rule
- `skills/printing-press-output-review/SKILL.md:10,21,64-80,82-117` — fork context, agent prompt contract, structured result block
- `internal/mcpdesc/compose.go:28,34,141-153,228-244` — description-shape caps + method markers
- `internal/generator/textfilter.go:18-27` — AI-slop regex (drift catcher pattern)
