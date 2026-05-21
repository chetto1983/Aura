# How Codex CLI enforces output discipline

Source: `github.com/openai/codex` (cloned `main`, May 2026) at `D:/tmp/codex/codex-rs/`. Aura analog: agent at `internal/agent/`, tools at `internal/tools/`, `execute_code` at `internal/tools/exec.go`.

## TL;DR

Codex stops "tool-dump" behavior with a stack: (1) explicit prompt rules forbidding raw output ("Don't dump large files... reference paths only", "relay important details... or summarize"); (2) a hard server-side **1 MB exec cap + 10 KB model-facing truncation** with a visible `…N chars truncated…` middle marker; (3) the **TUI shows only 5 lines** of any tool-driven shell output to the human (full output goes only to the model). The model is therefore *trained by context* to treat tool results as private working memory it summarizes, never as content to relay.

## Pattern 1 — Explicit "don't dump, summarize" prompt rule

`D:\tmp\codex\codex-rs\core\gpt_5_2_prompt.md:60` (Presenting your work):

> *"The user does not command execution outputs. When asked to show the output of a command (e.g. `git show`), relay the important details in your answer or summarize the key lines so the user understands the result."*

And `gpt-5.2-codex_prompt.md:53`:

> *"Don't dump large files you've written; reference paths only."*

And `gpt_5_2_prompt.md:166`:

> *"The user is working on the same computer as you, and has access to your work. As such there's no need to show the contents of files you have already written unless the user explicitly asks for them."*

**Why this fixes Aura.** Aura's `AGENT.md`/`SOUL.md` overlays do not contain an equivalent clause. Adding a single sentence — *"Tool outputs (execute_code, read_source, search_memory) are private working notes. When the user asks for data, summarize/synthesize the relevant entries; never paste rows verbatim unless the user explicitly says 'mostra tutta la tabella' or 'dump'."* — converts the 90-row dump into a synthesized 1–3 row reply.

## Pattern 2 — Hard verbosity budgets keyed to change size

`gpt_5_2_prompt.md:225-230`:

> *"Tiny/small single-file change (≤ ~10 lines): 2–5 sentences or ≤3 bullets. No headings. 0–1 short snippet (≤3 lines) only if essential. Medium change: ≤6 bullets or 6–10 sentences. Never include "before/after" pairs, full method bodies, or large/scrolling code blocks."*

And `:170`: *"Brevity is very important as a default. You should be very concise (i.e. no more than 10 lines)"*.

**Why this fixes Aura.** The 36-second wall-of-text reply violated no rule because Aura has no upper-bound rule. Codex couples the answer length to the *task complexity*, not the tool output size — "find one customer" maps to "tiny task → 2–5 sentences", which mechanically forbids dumping 90 rows.

## Pattern 3 — Two-tier output truncation: 1 MB capture, 10 KB model-facing

Capture cap: `D:\tmp\codex\codex-rs\utils\pty\src\lib.rs:10`
```rust
pub const DEFAULT_OUTPUT_BYTES_CAP: usize = 1024 * 1024;
```
applied in `codex-rs/core/src/exec.rs:68` `EXEC_OUTPUT_MAX_BYTES`, and aggregated at `exec.rs:865-907` (1/3 stdout, 2/3 stderr, rebalanced).

Model-facing cap (per-model in catalog, default **10,000 bytes**):
`D:\tmp\codex\codex-rs\protocol\src\openai_models.rs:604` and `model-provider/src/amazon_bedrock/catalog.rs:69`:
```rust
truncation_policy: TruncationPolicyConfig::bytes(/*limit*/ 10_000)
```

Applied at `core/src/tools/mod.rs:62-89` `format_exec_output_for_model()`:
```rust
sections.push(format!("Exit code: {}", exit_code));
sections.push(format!("Wall time: {} seconds", duration_seconds));
if total_lines != formatted_output.lines().count() {
    sections.push(format!("Total output lines: {total_lines}"));
}
sections.push("Output:".to_string());
sections.push(formatted_output);  // already truncated
```

Truncation is **middle-eliding** with an explicit marker (`utils/string/src/truncate.rs:131`):
```rust
fn format_truncation_marker(use_tokens: bool, removed_count: u64) -> String {
    if use_tokens { format!("…{removed_count} tokens truncated…") }
    else          { format!("…{removed_count} chars truncated…") }
}
```

**Why this fixes Aura.** Aura's `execute_code` returns the full Python stdout. For a 90-row print, the model literally sees the full table and has no incentive (or budget signal) to summarize. Wrapping the result through a `truncate_middle(text, 10_000_bytes)` helper that injects `…N chars truncated…` would (a) cap the worst case and (b) signal to the model "this was big, don't paste it back". Prepending `Total output lines: N` is the cheap header that lets Aura answer "the table has 90 rows; here's one" without re-reading the table.

## Pattern 4 — UI shows 5 lines; only the model sees the full payload

`D:\tmp\codex\codex-rs\tui\src\exec_cell\render.rs:32-33`:
```rust
pub(crate) const TOOL_CALL_MAX_LINES: usize = 5;
const USER_SHELL_TOOL_CALL_MAX_LINES: usize = 50;
```
Selected at `:442-461`: agent-driven shell calls clip to 5 lines, user-direct `!cmd` to 50. The model's `function_call_output` is independent of this UI clip — model gets truncated-to-10KB; user gets 5 lines + "view transcript" affordance.

**Why this fixes Aura.** Telegram has no expandable transcript. Today Aura's `execute_code` output flows straight into the assistant turn → model echoes it → user gets the full wall of text. Splitting the two channels — feed the (truncated) tool output **only** to the model, render to the user a one-line collapsed indicator like `eseguito script (90 righe)` — removes the dump path entirely. Aura already has the plumbing: the LLM's `tool_use`/`tool_result` blocks are server-side; the Telegram outbound (`internal/channels/telegram/outbound.go`) chooses what to render.

## Pattern 5 — Plan tool replaces "narrating the dump"

`gpt_5_2_prompt.md:38-46`:
> *"Plans can help to make complex, ambiguous, or multi-phase work clearer... Do not repeat the full contents of the plan after an `update_plan` call — the harness already displays it. Instead, summarize the change made and highlight any important context or next step."*

The `update_plan` tool (declared in the agent loop blog post and at `core/src/tools/handlers/` for plan tools) is the **structured channel** for "what I'm doing". The chat reply is reserved for the *result*.

**Why this fixes Aura.** Aura's chain-of-thought leaks into the user channel: the model "shows its work" by pasting the data. Adding a lightweight `agent_status` / `update_plan`-style tool (or repurposing existing reasoning-pane streaming, see `internal/channels/telegram/outbound.go` `ResetForNewRound`) gives the model a place to put "ho letto la tabella (90 righe)" *outside* the user-visible reply.

## Pattern 6 — "Final answer adapts to task shape" (anti-mechanical-formatting)

`gpt_5_2_prompt.md:240` and `gpt-5.2-codex_prompt.md:72`:
> *"Adaptation: code explanations → precise, structured with code refs; simple tasks → lead with outcome; big changes → logical walkthrough + rationale + next actions; casual one-offs → plain sentences, no headers/bullets."*

And `:162`: *"For casual conversation, brainstorming tasks, or quick questions from the user, respond in a friendly, conversational tone."*

**Why this fixes Aura.** "Trova un cliente e stampalo" is a **casual one-off** in Codex's taxonomy → plain sentences, no formatting, ≤10 lines. Without a taxonomy in Aura's overlay the model defaults to "thorough" mode (Italian assistants in particular over-format). One classification clause in `AGENT.md` collapses the failure mode.

## Pattern 7 — Tool description says "returns its output" — no formatting promises

`D:\tmp\codex\codex-rs\core\src\tools\handlers\shell_spec.rs:198`:
```rust
r#"Runs a shell command and returns its output.
- Always set the `workdir` param... Do not use `cd` unless absolutely necessary."#
```

Notable for what's **absent**: no promise that the output will be shown to the user, no "use this to retrieve data for the user". The tool description is purely *operational*. Result presentation is the model's responsibility, governed by the prompt rules above.

**Why this fixes Aura.** Aura's `execute_code` description in `internal/tools/exec.go` (and similar in `source.go` for `read_source`) tends to read like "use this to read/show data". Reframing as "runs Python and returns stdout for **your** analysis — synthesize a reply for the user" shifts the model's mental model from "fetch + relay" to "fetch + reason + reply".

## Pattern 8 — Persistence + autonomy rule blocks "narrate-and-stop"

`gpt_5_2_prompt.md:30-32`:
> *"Persist until the task is fully handled end-to-end within the current turn... Unless the user explicitly asks for a plan, asks a question about the code, is brainstorming... assume the user wants you to make code changes or run tools to solve the user's problem. In these cases, it's bad to output your proposed solution in a message, you should go ahead and actually implement the change."*

**Why this fixes Aura.** Aura's failure was the **inverse** problem — narrate too much. But the same lever (explicit persona/autonomy rules) applies in reverse: a clause *"the user does not want the raw tool output; they want the answer. If `execute_code` returned a list, pick the one(s) relevant to the question and reply with those, not the list"* converts the model from "show the table" to "answer the question".

---

## Minimal Aura patch sketch (in priority order)

1. Edit `AGENT.md` / `SOUL.md` overlays — add the 3 rules from Patterns 1, 2, 6.
2. Add a `truncate_middle(out, 10_000)` wrapper in `internal/tools/exec.go` `execute_code` result path; prepend `Total output lines: N\n` when truncated. Mirror in `read_source`, `read_memory`, `search_memory`.
3. Telegram outbound: clip raw tool-output rendering to 5 lines (or replace with `eseguito (N righe)` placeholder) — keep full output in the model channel only (Pattern 4).
4. Tool descriptions: edit `execute_code` / `read_source` / `search_memory` to end with "*Result is for the model's analysis. Synthesize a reply; do not paste the raw output.*"
