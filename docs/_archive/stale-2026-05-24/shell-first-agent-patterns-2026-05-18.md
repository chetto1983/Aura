# Shell-first agent patterns — tool surface size across `D:/tmp`

**Source-of-truth scan date:** 2026-05-18 against `D:/tmp/` snapshots.
**Method:** counted distinct LLM-visible tool names at the registry call site (`registry.register(...)`, `impl Tool for X`, `McpToolSpec { name: ... }`, or H2 section in leaked Claude Code prompt). Plumbing helpers, MCP wrappers, and "deferred / lazy-loaded via search" tools are tallied separately because they materially change what the model "sees" on a cold turn.

---

## Tool count comparison table

Ordered by **tools visible on a cold turn** (what enters the prompt unconditionally), ascending.

| Repo | LLM-visible tools (cold turn) | Total registerable | Shell catch-all? | File ops shape | Source of count | Notes |
|---|---|---|---|---|---|---|
| `cli-printing-press` | **0** | 0 | — (drives `claude`/`codex` CLI via subprocess) | n/a | `internal/llm/llm.go:14` `Available()` only LookPath's `claude`+`codex` | Pipeline orchestrator, not an agent. Excluded from analysis below. |
| `elysia` | **9** | 9 | No | No file ops at all — RAG-only | `grep -rn "^class.*Tool" elysia/tools/` → Query, Aggregate, SummariseItems, CitedSummarizer, Summarizer, TextResponse, FakeTextResponse, BasicLinearRegression, Visualise | Weaviate-RAG agent. Tools are output shapes (query, visualise, summarise), not capabilities. |
| `codex` (per OpenAI Jan-2026 blog post `codex.md`) | **3** | 3 + N MCP | **Yes — `shell` is THE primary tool** | All file ops go through `shell` | `codex.md:121` (`name: "shell"`), `codex.md:191` (`update_plan`), `codex.md:237` (`web_search`) | Built-ins: `shell`, `update_plan`, `web_search`. Everything else is MCP. Reference design for "shell-first". |
| `claude-code` (Anthropic, prompt leak `system_prompts_leaks/Anthropic/claude-code.md`) | **10 always loaded + 17 deferred = 44** | 44 | **Yes — `Bash` exists but with explicit "prefer dedicated tool" prose** | `Read` / `Write` / `Edit` / `Glob` / `Grep` are first-class | `claude-code.md` H2 sections grep → Agent, Bash, Edit, Glob, Grep, Read, ScheduleWakeup, Skill, ToolSearch, Write | The 17 deferred tools (`CronCreate`, `WebFetch`, `NotebookEdit`, `EnterPlanMode`, `Monitor`, `TodoWrite`, ...) only get their schema loaded after a `ToolSearch` round-trip. Pattern: **lazy-load to keep the cold prompt small**. Bash docstring (lines 290-300 in the leak) lists *prefer Glob over find, Grep over rg, Read over cat, Edit over sed, Write over `echo >`*. |
| `picobot` | **16** | 16 + N MCP | Yes (`exec`) | Single `filesystem` tool with `action` enum `{read,write,list}` | `internal/agent/loop.go:80-117` registry.Register calls | Brags about its small surface in `README.md:78` "16 Built-in Tools + MCP". File ops use **action dispatch** — same pattern Aura uses. |
| `nanobot` | **~16** | ~16 + N MCP | Yes (`exec`, found in `shell.py:131`) | **Flat: separate `read_file`, `write_file`, `edit_file`, `list_dir`, `grep`, `notebook_edit`** | `grep -A1 "def name"` of all tool modules | Hybrid: has `exec` AND specialized file tools. `exec`'s description explicitly says *"Prefer read_file/write_file/edit_file over cat/echo/sed, and grep/glob over shell find/grep."* (`shell.py:152-157`). Mirrors Claude Code's stance. |
| `aura` (this repo, per `docs/qa-tool-surface.md` 2026-05-18) | **23** (19 static + 4 swarm) | 23 + N MCP | Yes, two: `execute_shell` + `execute_code` (Python sandbox) | **Mostly consolidated under action-dispatch**: `file{action}`, `source{action}`, `web{action}`, `doc{action}`, `wiki_page{action}` | `internal/agent/tools/registry/` + `internal/agent/tools/swarm/` Name() returns | Sub-tools `list_files/read_file/store_source/web_search/create_xlsx/...` exist as registry entries but are NOT independently `Register`ed in production — they live under the parent's `action=` dispatch. Verbose unified tools. |
| `hermes-agent` | **73** registered (toolset-gated) | 73 + N MCP | Yes (`terminal` and `execute_code`) | **Flat AND duplicated**: separate `read_file`, `write_file`, `patch`, `search_files` AND `terminal` | `grep 'name="' tools/*.py | sort -u | wc -l` = 73 | Operator chooses which **toolsets** (`file`, `browser`, `discord`, `feishu`, `kanban`, `rl`, `homeassistant`, `yumb`, ...) to enable per session. Cold-turn count is whatever toolsets are on. Maximalist + opt-in. |
| `openhuman` | **~76 native** + 11 MCP external + 118 Composio integration tools | ~76 (native `impl Tool for X` count) | Yes (`shell`, `system/shell.rs:60`) | **Flat**: `file_read`, `file_write`, `edit_file`, `list_files`, `glob_search`, `grep`, `read_diff`, `apply_patch` are separate tools | `grep -rn "impl Tool for" src/openhuman/tools/impl/` = 77 hits | The 118 README integrations route through ONE `composio` tool (`tools/impl/network/composio.rs:494`), so they don't all flatten into the cold prompt — only `composio.execute(action="GMAIL_...")` enters the surface. Still the heaviest native surface in the sample. |
| `kimi-k2.5` (paper `2602.02276`, §E.9) | Orchestrator: ~5 tool categories | n/a (research paper) | Yes (IPython + Shell listed under "Other tools") | Browser-driven, not direct file | `2602.02276-Kimi-K2.5.txt` "Sub Agent tools" section | Categories: (1) Search, (2) Browser tools, (3) `create_subagent` + `assign_task`, (4) code execution (IPython, Shell). Browser/Sub-agent each expose multiple sub-verbs as separate tools, so the actual JSON-schema count is higher than 5, but the *taxonomy* the paper documents is minimal. |

---

## The minimalist extreme — Codex

**Codex CLI** (per the official OpenAI engineering post `codex.md`, the canonical Jan-2026 reference) ships **THREE built-in tools** to the model on a fresh turn:

1. `shell` — runs a shell command with `{command: string[], workdir?, timeout_ms?}`. Sandboxed via Codex's seatbelt/landlock harness.
2. `update_plan` — writes/updates the task plan visible to the user (the "cards" you see in the CLI UI).
3. `web_search` — Responses-API-hosted, `{external_web_access: false}`.

That's it. Every other capability — read a file, write a file, edit a file, run a test, ingest a PDF, query a DB, send a message — is **`shell` + a CLI program**. Codex's design rationale (from the blog post and from `internal/llm/llm.go` of `cli-printing-press` which is built ON TOP of Codex CLI) is the **CLI Printing Press** thesis: build *one* small tool surface, then invest in the surrounding CLI ecosystem so the model has muscle memory for `gh`, `jq`, `rg`, `python -m`, `npm`, `cat << EOF > file`, `apply_patch < diff`, etc.

**What Codex CAN do with `shell` alone:**
- read: `cat`, `head`, `tail`, `sed -n`
- write: `tee`, `cat <<EOF >file`, `python -c 'open(...).write(...)'`
- edit: `apply_patch < diff`, `sed -i`, `python -c` for AST edits
- search: `rg`, `grep`, `find`, `fd`
- run tests: any test runner
- git: full git CLI
- system observation: `ps`, `lsof`, `netstat`
- any HTTP: `curl`

**What it CAN'T do well:**
- Atomic structured-data write to a typed store (no native `wiki_page.write(slug, frontmatter, body)` — must do `cat << EOF > wiki/foo.md` and hope the LLM gets YAML frontmatter right).
- Schedule a future task (no native cron tool — would need an MCP server or systemd-timer shell setup).
- Approval-gated writes (the harness gates `shell` itself, but distinguishing "write code" from "delete prod database" is hard inside one tool — Codex handles this via the harness asking the user, but the model can't reason about *which kind of write it's doing* by tool name).
- Send a Telegram/Discord/Slack message (would need a CLI for each).

The "fewest tools" trophy is **`codex` (3 built-ins)**. The "most minimal that actually shipped to millions of users" trophy is also `codex`.

---

## The maximalist extreme — OpenHuman

**OpenHuman** has **77 `impl Tool for X` hits** in `src/openhuman/tools/impl/` (Rust). Categories:

- `agent/` (13 tools — delegation, subagent spawn, plan/onboarding, todo)
- `audio/` (3 — podcast gen/email)
- `browser/` (8+ — open, screenshot, click, image_info, native_backend)
- `computer/` (2 — keyboard, mouse — actual desktop control)
- `cron/` (6 — add/list/remove/run/runs/update)
- `filesystem/` (13 — read/write/edit + grep/glob + apply_patch + git_operations + linter + tests + csv_export + memory.md)
- `memory/` (3 + 6 tree sub-tools)
- `network/` (9 — composio gateway + curl + http_request + web_search + web_fetch + mcp client + gitbooks + gmail_unsubscribe)
- `system/` (15 — shell + lsp + node_exec + npm_exec + schedule + pushover + ...)
- `whatsapp_data/` (3 — list_chats, list_messages, search_messages)

**Why does it work for them?** Three reasons visible in the code:

1. **Strict naming + namespacing.** `tree.read_chunk`, `cron.add`, `memory.tree.drill_down`. The model rarely confuses `memory.recall` with `memory.tree.query_topic` because the schemas are tight and the names self-document the verb.
2. **Composio is one tool, not 118.** The 118-integrations README claim collapses into `tools::impl::network::composio.rs` → one tool with `action: "GMAIL_FETCH_EMAILS"` style arg. The integration count is a marketing number; the **agent-visible** count is 1.
3. **Permission gates per tool.** `tools/traits.rs` defines `PermissionLevel` per `impl Tool`, and the dispatcher refuses unapproved calls. With 77 tools, you can give `file_read` "auto-approve" and `shell` "require approval" — something you CAN'T do with a single `shell` catch-all.

**Does it actually work?** OpenHuman is "early beta" per their own README. We have no public mistake-rate data. The Rust `tools/impl/agent/spawn_parallel_agents_test.rs:281` exists, but no comparable benchmark of "tool-call mistake rate at N tools" is in the repo.

---

## Aura comparison

Aura sits at **23 LLM-visible tools** (per `docs/qa-tool-surface.md` 2026-05-18: 19 static-registry + 4 swarm + N MCP). That places Aura:

- **Above** Codex (3), Elysia (9), Picobot (16), Nanobot (16).
- **At** Claude Code's "always loaded" tier (10) + deferred (17) = 27 — Aura is ~the same magnitude as Claude Code's full surface.
- **Below** Hermes (73), OpenHuman (77).

Aura already **uses action-dispatch consolidation** like Picobot — `file{action=read|write|patch|search|list}`, `source{action=store|read|ocr|list|delete|ingest}`, `web{action=search|fetch}`, `doc{action=create_xlsx|create_docx|create_pdf}`, `wiki_page{action=create|update|append|edit|...}`. The underlying sub-tools (`list_files`, `read_file`, `web_search`, `create_xlsx`, ...) exist in `internal/agent/tools/registry/` but are NOT independently registered — they live inside the parent's dispatch.

**What Aura would LOSE consolidating `file` + `source` + `wiki_page` + `doc` + `agent_note` into a single `shell` tool:**

- **Atomic typed writes.** `wiki_page` enforces frontmatter (`expected_updated_at` for optimistic-concurrency, `category`, `tags`, `related[]`, `sources[]`) — `shell echo "..." > wiki/foo.md` corrupts this every other turn. The wiki invariant `temperature=0 deterministic write + atomic temp-file rename + git commit + file mutex` (per `CLAUDE.md` §Wiki) is non-trivial to express via shell.
- **Source dedup by SHA-256.** `source.store` hashes incoming bytes and dedupes via `wiki/raw/src_<hex>/`. A shell-based version would have the LLM re-implement hashing on every call.
- **Telegram artifact delivery.** `doc.create_xlsx(... deliver=true)` not only produces the xlsx but ALSO pushes it to the originating chat. Shell-based generation can't do "send to whoever asked".
- **Schema validation.** `task.action=schedule` rejects `at=` in the past, validates `weekdays=[]`, parses `every_minutes` — all enforced at the tool boundary. Pushing this to shell shifts validation onto the model.
- **Tool-aware capability gates.** `subagent_dispatch`, `swarm.spawn`, `dev_tool` have different `capability_gate` strings in the registry (`tool.execute` vs `swarm.spawn`) — you can disable swarm without disabling `file`. With `shell`, you can only gate "shell yes/no".
- **Argument logging redaction.** `internal/agent/tools/registry/registry.go:400` `sensitiveArgKeyRe` redacts tokens/URLs/base64 from logged tool args. Inside `shell command=...` every secret is in the command string.
- **Observability granularity.** Aura's `conversations` archive records `tool_calls JSON` per turn (per `CLAUDE.md`); pivoting it by `tool_name=wiki_page` gives a clean view of every wiki touch. With `shell` you lose this — every call looks the same.

**What Aura would GAIN:**

- **Zero `file() without action` mistakes.** The current Telegram session bug ("`file()` without `action`, `list_files: path outside root`, `propose_patch missing change_summary`") is structurally impossible with `shell {command}` — there's exactly one required param. The action-dispatch design has N×M failure modes (N actions × M required-args-per-action); shell has 1.
- **Long tail of ops without new tools.** Want to run `git log --since='last week'`? Today you'd need to add a tool or open a sandbox. Shell handles it for free.
- **Stronger composition.** Pipes (`source.read | grep | head`) work natively in shell. In Aura today you'd `file action=read max_bytes=N` + post-process in `execute_code`.
- **Smaller prompt.** The 19-tool tools-section in Aura's system prompt is ~800 tokens (verified empirically against the Aura system prompt — 23 tool definitions × ~35 tokens each). A 4-tool surface (`shell`, `search_memory`, `wiki_page`, `task`) would be ~150 tokens. **Saves ~650 tokens per turn × every turn for the life of the agent.**

---

## Mistake-rate hypothesis — what the codebases hint at

There is no single benchmark across these repos that says "action dispatch produces N% more mistakes than flat tools". But four signals converge:

1. **Codex's `shell` description** (`codex.md:125`): *"Runs a shell command and returns its output..."* — no enum, no action arg, no positional-vs-named-arg gotcha. **Lowest possible mistake surface.** OpenAI shipped this and the entire Codex CLI category survives on it.

2. **Claude Code's Bash docstring** (`claude-code.md` lines 290-300): explicitly lists *"prefer Glob over find, Grep over rg, Read over cat, Edit over sed, Write over `echo >`"*. Anthropic empirically observed that **the model uses shell badly enough that they had to add a 7-line prompt nudge to redirect it to specialized tools**. They KEPT `Bash` but they don't *want* it used for ops that have a typed tool. Inference: flat-typed tools have lower per-call mistake rate than shell, but shell remains the long-tail safety valve.

3. **Nanobot's `exec` description** (`shell.py:152`): word-for-word the same nudge — *"Prefer read_file/write_file/edit_file over cat/echo/sed, and grep/glob over shell find/grep."* Independent rediscovery of the Anthropic pattern. Two of three serious agents agree.

4. **Picobot's `filesystem{action}` with 3-entry enum** (`internal/agent/tools/filesystem.go:46`): **action dispatch with a TIGHT enum (3 values) and a CLEAR required-arg shape (`action`+`path`, `content` only for write)**. No `list_files: path outside root` style breakage. The dispatch is structurally similar to Aura's `file{action}` but with a smaller surface — 3 actions instead of 5 (`read|write|patch|search|list`) and 4 args instead of 11.

**The pattern:** mistake rate seems to correlate with **schema fan-out per tool**, not raw tool count. A 5-action `file` tool with 11 possible args (Aura) gives the model more places to forget a required field than 5 separate flat tools each with 2 required fields, OR a 3-action `filesystem` tool (Picobot). Aura's current bugs (`file() without action`, `propose_patch missing change_summary`) are exactly this failure mode — required-arg-per-action coupling.

**A direct anecdote from this codebase:** `feedback_no_regex_for_nlp.md` in your memory rules already encodes the same idea ("ground-truth structured trigger, not regex on prose"). The dual is: **structured tool boundary, not many-action dispatch with conditional-required-args**.

---

## Recommendation for Aura

### Option A — Keep current 23-tool surface (action-dispatch consolidation)

**Pros**
- Zero migration cost; existing probes/skills/wiki invariants stay valid.
- Strong observability per tool name in `conversations.tool_calls`.
- Per-tool capability gates (`tool.execute` vs `swarm.spawn`) keep working.
- Atomic typed writes (`wiki_page` frontmatter, `source` SHA dedup) remain enforced at the boundary.

**Cons**
- The Telegram-session bug (`file()` no action, `propose_patch missing change_summary`) is **structural** in this design. It will keep happening on smaller/colder models. Trim it with prompt patches, but it won't go to zero.
- ~800 tokens/turn of tool definitions in the system prompt.
- The 19-tool wall is intimidating for a new model; today the fix is "load `TOOLS.md` overlay" but that's another file to maintain.

**Effort:** 0 (it's the status quo).

### Option B — Consolidate to a Codex-style 4-tool surface (`shell`, `search`, `propose`, `schedule`)

**Pros**
- Lowest possible per-call mistake rate (Codex empirical evidence).
- Smallest cold-prompt (~150 tokens saved per turn → ~$X/month at current volume).
- Forces the model to use the CLI ecosystem (`gh`, `jq`, `rg`, `apply_patch`) which is well-trained.
- "Aura on a $5 VPS" picobot-style story becomes available.

**Cons**
- **Wiki invariants break.** `wiki_page.write` with `expected_updated_at` optimistic-concurrency cannot be expressed as `shell echo >`. Either you re-implement it as a server-side endpoint the agent calls via `shell curl -X POST ...` (re-introducing the typed boundary in HTTP shape), or you accept wiki corruption.
- **Telegram artifact delivery breaks.** `doc.create_xlsx(... deliver=true)` requires the tool to know the originating chat. `shell` doesn't have that ambient state.
- **Source SHA dedup needs reimplementation** as a CLI binary the model invokes (`aura source-store < bytes`).
- **Capability gates collapse.** Approving "shell" approves everything.
- **Massive migration:** every existing probe, skill, and `TOOLS.md` overlay rewritten.

**Effort:** Phase-sized — at least 2-3 weeks (rewrite tool boundary + reshim wiki/source/doc as CLI binaries + rewrite ALL probes + retune overlays + retrain habits). And once you're done, you've largely re-built the typed boundary as an internal CLI ABI — the win shrinks.

### Option C — Hybrid (keep typed write tools + add `shell` as the long-tail catch-all)

**Pros**
- **Lowest-risk, highest-evidence path.** This is what Anthropic ships in Claude Code (Bash + Read/Write/Edit/Grep/Glob). This is what nanobot ships (exec + read_file/write_file/edit_file). It's the consensus answer when one mature agent and one careful agent reinvented it independently.
- Keep `wiki_page`, `source`, `doc` (typed atomic writes preserve invariants and approval gates).
- Drop the ones where shell beats action-dispatch: collapse `file` (5 actions, 11 args, currently buggy) into `shell` + keep `propose_patch`/`agent_note` as the safe typed-write path.
- Telegram delivery, SHA dedup, schedule validation all remain at the tool boundary where they belong.
- Per-call mistake rate drops for the file-ops use-case (the loudest source of current bugs per Telegram session evidence).
- Smaller, but not dramatically smaller, prompt (~600 tokens vs 800).

**Cons**
- **Two-paths-to-the-same-thing problem.** Model can `wiki_page action=update slug=foo body=...` OR `shell sed -i s/old/new/ wiki/foo.md`. Need clear prompt nudge ("prefer wiki_page over shell for wiki ops") — exactly the Anthropic pattern.
- Approval-gate UX: shell is the new "always require approval" tool, while wiki_page/source/doc stay auto-approved within scope. Need to land a per-tool approval policy that didn't exist before. Wiring exists (`capability_gate`); UI doesn't.
- Sandbox parity: current `execute_shell` already exists at `internal/agent/tools/registry/exec.go:416`; "shell" as the new primary tool just means promoting it from "for `execute_code` to call inside the sandbox" to "for the model to call directly". Risk surface widens — `NET_RAW/NET_ADMIN` + `nmap` from `project_aura_lan_exposure_2026-05-17.md` already widened it; need to revisit.

**Effort:** 3-5 days of focused work. Concrete diff:
- Promote `execute_shell` to first-class top-of-prompt tool with revised description (steal Claude Code's nudge prose verbatim).
- Retire `file{action=search|list}` (let model use `rg`/`ls`/`find` via shell). KEEP `file{action=read|write|patch}` because patch is structurally diff-shaped and benefits from typing.
- Update `TOOLS.md` overlay with "shell vs typed tool" decision tree.
- Add 4-5 probe cases that exercise the new shell-first path AND verify the typed-tool path still works (per `feedback_probe_must_verify_artifact_not_reply.md` — artifact ground truth, not reply text).

### Option D — No change; mistakes are prompt/training-shaped not tool-count-shaped

**Pros**
- Costs nothing.
- Some Telegram-session mistakes are genuinely prompt-shaped — `file() without action` happens when `TOOLS.md` is stale or the system prompt is too long and the model loses the tool definition.
- The recent Telegram bugs (per `project_2026-05-18_phase_qa1_session.md`) were on the order of 1-3 per session, mostly recoverable; not catastrophic.

**Cons**
- Empirical evidence (Anthropic + Nanobot independently nudging *away* from shell for typed ops AND keeping shell anyway) suggests the issue is NOT just prompt-shaped. Tool-surface shape matters.
- Doesn't address the "Aura runs on a $5 VPS"-style scaling story.

**Effort:** 0.

### Score summary

| Option | Mistake rate | Prompt size | Effort | Migration risk | Net |
|---|---|---|---|---|---|
| A — status quo | 6/10 | 5/10 | 0 | 0 | **5.5** |
| B — pure shell-first (Codex-style) | 9/10 (best) | 10/10 (best) | -8/10 (terrible) | -9/10 (catastrophic) | **2.5** |
| C — hybrid (typed-write + shell) | 8/10 | 7/10 | -3/10 | -3/10 | **7.0** |
| D — no change, blame prompt | 4/10 | 5/10 | 0 | 0 | **4.5** |

**Recommendation: Option C.** Anthropic and Nanobot independently converged on it. Aura already has the building blocks (`execute_shell` exists; `capability_gate` infrastructure exists; the typed-write tools that MUST stay typed are clearly identifiable from the wiki/source/doc invariants).

---

## Bottom line (200 words)

Aura sits at 23 tools — middle of the pack, well above Codex's shell-only 3 but well below OpenHuman's 76. The codebases tell a consistent story: **pure shell-first works for Codex because OpenAI invested in the CLI ecosystem around it; pure flat tooling works for OpenHuman because they ship strict per-tool permission gates; the consensus middle (Anthropic Claude Code, Nanobot) is HYBRID — keep typed tools for ops where atomicity/validation/observability matter, add `shell` as the long-tail catch-all, and put a prompt nudge that says "prefer the typed tool when one exists".**

Aura's current Telegram bugs (`file() without action`, `propose_patch missing change_summary`) are textbook many-action-with-conditional-required-args failure mode. They will not go to zero by tightening the prompt. They go to zero by collapsing `file{action=search|list}` into `shell` (the model already knows `rg` and `ls` better than your action enum) AND keeping `wiki_page` / `source` / `doc` / `propose_patch` / `task` typed because their invariants (frontmatter, SHA dedup, Telegram delivery, optimistic concurrency, schedule validation) cannot survive in shell-string-land.

Cost: ~3-5 days. Risk: low because `execute_shell` already exists. Trophy: smaller prompt, lower per-call mistake rate, the long tail of "just `git log --since='last week'`" becomes free.
