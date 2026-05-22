# D:/tmp Module-Shape Survey — Aura Cleanup Reference

**Date:** 2026-05-22
**Scope:** Survey of MODULE STRUCTURE across 8 curated production agent repos (`codex`, `elysia`, `nanobot`, `picobot`, `openhuman`, `hermes-agent`, `cli-printing-press`, `graphify`).
**Goal:** identify how mature production agent systems organize their internal modules, to inform Aura's module-by-module cleanup (~83k LOC Go monorepo under `internal/`).

Method: read SOURCE (no READMEs); count files + LOC per module; inspect entrypoints, visibility, tests, lint configs.

---

## 1. codex (Rust, Bazel + Cargo workspace, ~440k LOC)

**Language:** Rust. Path: `D:/tmp/codex/codex-rs/`. Cargo workspace with **~60+ member crates** (cli, core, app-server, codex-mcp, connectors, features, secrets, exec, exec-server, execpolicy, skills, hooks, ext/extension-api, ext/goal, ext/guardian, ext/memories, etc.).

1. **Module count + size.** `core/` crate alone has 337 source files = **152,994 LOC**; biggest file `core/src/config/config_tests.rs` is 11,778 LOC; session/tests.rs 10,232 LOC. `app-server` = 67 files / 39k. `core-plugins` = 33 files / 21k. Tiny crates exist alongside (`core-api` = 1 file / 79 LOC). Median crate file count ~5-15.
2. **Boundaries.** Explicit and ENFORCED via Cargo crate boundaries + Rust `pub`/`pub(crate)`/`mod` discipline. `core/src/lib.rs` lists every internal `mod` with explicit visibility — e.g. `pub(crate) mod session; pub use session::SteerInputError;` exports a single symbol while the whole module is crate-private. Hard.
3. **Test coverage shape.** Mostly **co-located**: `foo.rs` + `foo_tests.rs` next to each other (e.g. `config/config_tests.rs`, `agent/control_tests.rs`, `mcp_tool_call_tests.rs`). Workspace-level test runners use a Bazel `workspace_root_test_launcher.sh.tpl`. Mocking via trait objects. **235 test files in 1996 total** (~12% test files but tests are MASSIVE — config_tests.rs alone is 11,778 LOC).
4. **LOC discipline.** None visible; `clippy.toml` only restricts **API surface** (no `Color::Rgb`, no `print_stdout/stderr` in lib code) and `large-error-threshold = 256` for error sizes. Real median file is several-hundred LOC; the test files are gargantuan.
5. **Dead-code hygiene.** 30 TODO/FIXME in 153k LOC of core = **~1 per 5,000 LOC**, very clean. No `_old.rs`/`.bak`. Some legacy variants kept side-by-side (`compact.rs`, `compact_remote.rs`, `compact_remote_v2.rs`) but each marked.
6. **Dependency direction.** `core` is the giant hub; everything else depends on it. `core-api` is the 79-LOC trait crate that lets `core-plugins` + `core-skills` depend on a stable interface instead of full core. **Explicit "api" split for plugin contracts is the pattern.**
7. **Surprised by.** (a) `core-api` is a 1-file 79-LOC crate whose only job is to expose the trait surface for plugins → other crates depend on `core-api`, not on `core`. (b) `ext/extension-api`, `ext/goal`, `ext/guardian`, `ext/memories` are **separate crates for extensions** — Aura merges all this in `internal/agent`. (c) `compact_remote.rs` + `compact_remote_v2.rs` kept side-by-side, gated by feature.
8. **Conventions.** `tracing` crate everywhere; `anyhow::Result` in lib code, custom error enums at crate boundaries; workspace.toml shared deps; `#![deny(clippy::print_stdout, clippy::print_stderr)]` in `core/lib.rs` to forbid raw IO.

**Aura takeaway (1 paragraph):** Codex proves that **explicit API-crate splits scale** — a tiny `core-api` crate (79 LOC) lets plugins/skills/extensions depend on a stable trait surface without dragging in the 153k-LOC `core`. Aura's `internal/llm`, `internal/tools`, `internal/wiki` should each emit a thin "contract" file (interfaces only, ~50-200 LOC) that other packages import, so the implementation can grow without recursive imports.

---

## 2. elysia (Python, Weaviate-backed agent platform, ~18k LOC)

**Language:** Python. Path: `D:/tmp/elysia/elysia/`. 5 subpackages: `api`, `preprocessing`, `tools`, `tree`, `util`.

1. **Module count + size.** `api/` = 35 files / 5,632 LOC; `tools/` = 21 files / 4,770 LOC; `tree/` = 5 files / 4,137 LOC; `util/` = 10 files / 2,525 LOC; `preprocessing/` = 3 files / 1,563 LOC. Largest single file: `tree/tree.py` = **2,153 LOC** (the entire agent loop in one file). `objects.py` at the root = 951 LOC (Tool metaclass + shared types).
2. **Boundaries.** Soft — Python conventions. No `__all__` in scanned files. Direct cross-module imports (e.g. `from elysia.tools.retrieval.aggregate import Aggregate` inside `tree.py`). No internal/private package marker.
3. **Test coverage shape.** Centralized at `tests/` (top-level, NOT co-located). Two test sub-roots: `tests/no_reqs/` (no live Weaviate) and `tests/requires_env/`. Uses pytest + conftest.py. Mocking via dspy/litellm fakes.
4. **LOC discipline.** None. The single biggest file (`tree.py`) is 2,153 LOC; `tree/objects.py` is 951 LOC.
5. **Dead-code hygiene.** **1 TODO/FIXME in the whole package.** Extremely clean. No `.bak`/`_old`.
6. **Dependency direction.** `tree/` is the orchestrator; pulls `tools/` + `util/` + `config` + root `objects.py`. `api/` is the FastAPI surface and depends on everything. No arch tests visible.
7. **Surprised by.** (a) Putting the **entire decision tree (agent loop) in one 2,153-LOC `tree.py`** but keeping `objects.py` separate at the root for `Result`, `Tool`, `Completed`, `Text`, etc. — a "shared protocol object" file. (b) `ToolMeta` metaclass in `objects.py` extracts tool metadata from `__init__` — Aura has nothing equivalent; tools are pure structs. (c) DSPy-driven prompts as first-class — no `prompts/*.md` overlay system.
8. **Conventions.** DSPy `Signature` classes for all LLM calls; `Settings` dataclass injected; `print()` from `rich` everywhere (no log abstraction); `AsyncGenerator` is the streaming contract.

**Aura takeaway:** Elysia's `objects.py` is the **central protocol file** every other module imports for `Result`/`Tool`/`Text` types — a one-stop-shop for the shared vocabulary. Aura's tool ABI types are split across `internal/agent/tools/registry/`, `internal/tools/`, and `internal/agent/`; consolidating to one `internal/agent/contract.go` (~200 LOC) would mirror this and stop the circular-import dance.

---

## 3. nanobot (Python, ~58k LOC, multi-channel agent)

**Language:** Python. Path: `D:/tmp/nanobot/nanobot/`. 19 subpackages: `agent`, `api`, `bus`, `channels`, `cli`, `command`, `config`, `cron`, `heartbeat`, `pairing`, `providers`, `security`, `session`, `skills`, `templates`, `utils`, `web`, `webui`.

1. **Module count + size.** Biggest: `channels/` = 19 files / **15,969 LOC** (one file per platform — `feishu.py` 1917, `websocket.py` 1729, `weixin.py` 1546, `telegram.py` 1300, `signal.py` 1402, etc.). `agent/` = 34 files / 12,605 LOC. `providers/` = 16 files / 6,926 LOC. `utils/` = 19 files / 3,574 LOC. Smallest active: `bus/` = 3 files / **98 LOC** (events.py 48 + queue.py 44).
2. **Boundaries.** Soft (Python). Cross-module imports without restraint.
3. **Test coverage shape.** **Tests NOT co-located** — separate `tests/` directory mirrors the package tree (`tests/agent/`, `tests/channels/`, `tests/providers/`, etc.) with per-module `conftest.py`. 212 test files. Inside `nanobot/` itself, **zero `test_*.py` files**. Mocking via pytest fixtures + httpx mock + custom Fake providers.
4. **LOC discipline.** None visible. Channel files routinely 1,000-2,000 LOC.
5. **Dead-code hygiene.** **8 TODO/FIXME total**. Very clean.
6. **Dependency direction.** `bus/` is the spine (98 LOC) — `agent`, `channels`, `command`, `session` all import `nanobot.bus.events` / `nanobot.bus.queue`. The smallest module is the most-depended-on (good).
7. **Surprised by.** (a) **`bus/` is 98 LOC and central** — proves the "tiny core eventbus" pattern works. (b) `agent/loop.py` is 1,613 LOC and imports from `agent.autocompact`, `agent.context`, `agent.hook`, `agent.memory`, `agent.progress_hook`, `agent.runner`, `agent.subagent`, `agent.tools.*` — the agent loop is one file but the **agent module is split into ~12 capability files** behind it. (c) `webui/` is 5 files / 1,427 LOC, separate from `web/` (8 LOC stub) — they renamed but didn't delete.
8. **Conventions.** `loguru` everywhere (single logging interface); `pydantic` BaseModel for all DTOs; `AsyncExitStack` for resource lifetime; `typer` for CLI.

**Aura takeaway:** Nanobot's `agent/` split mirrors what Aura needs — one **`loop.py` (orchestrator) + ~12 capability files** (autocompact, context, hook, memory, progress_hook, runner, subagent, tools/). Aura's `internal/agent/` could decompose `loop.go` into a thin orchestrator that imports capability files (compact.go, context.go, memory.go, runner.go) — exactly what's already half-started.

---

## 4. picobot (Go, ~9k LOC, smallest reference)

**Language:** Go. Path: `D:/tmp/picobot/`. **9 internal modules** = the cleanest layout in the survey.

1. **Module count + size.** Total ~8,905 LOC across 9 modules. `agent/` = 18 src + 21 test = 4,484 LOC. `channels/` = 5 + 4 = 2,342 LOC. `config/` = 641. `mcp/` = 538. `providers/` = 396. `cron/` = 255. `session/` = 101. `chat/` = 99. `heartbeat/` = **49 LOC**. **No file exceeds 500 LOC** (biggest: `whatsapp_test.go` 486, `whatsapp.go` 449, `onboard.go` 392, `agent/loop.go` **369**).
2. **Boundaries.** Go `internal/` enforces crate-style visibility — hard. Every package has a clean import-list at the top of each file; no transitive leaks.
3. **Test coverage shape.** **Co-located Go convention** — `*_test.go` next to source. 32 test files / 69 total = **46% are tests**. Highest of any repo surveyed. Mocking via interfaces in `providers/provider.go` + `mcp/client.go`.
4. **LOC discipline.** Implicit: nothing >500 LOC. `agent/loop.go` (the agent loop) is **369 LOC** — Aura's equivalent is multiple-x bigger.
5. **Dead-code hygiene.** **2 TODO/FIXME total** in 9k LOC. Cleanest in the survey.
6. **Dependency direction.** `chat/` (99 LOC) is the bus; `cmd/picobot/main.go` is the composition root that imports `agent`, `channels`, `chat`, `config`, `cron`, `heartbeat`, `providers` and wires them. Strict DI from main. No cyclic imports.
7. **Surprised by.** (a) **A 49-LOC `heartbeat/` module survives** — proves "small modules are OK" when responsibility is single. Aura tends to absorb tiny concerns into bigger modules. (b) `chat/chat.go` is 99 LOC and is the entire chat-Hub interface — Aura's equivalent is split across `internal/chat/` + `internal/channels/*/outbound.go` + `internal/agent/` event types. (c) `agent/tools/` is its own subpackage with `skill.go`, `memory.go`, `memory_test.go` — separate from `agent/` top-level.
8. **Conventions.** stdlib `log` (no zap); plain `error`; `context.Context` first arg; `config.Config` injected via main; no global state.

**Aura takeaway:** Picobot is the **size-discipline reference**. Its `agent/loop.go` does what Aura's loop does in 369 LOC. Aura should treat picobot's structure (9 modules, all <500 LOC files, 46% test ratio) as the **lower-bound proof point**, and apply its discipline (one file = one responsibility, no file >500 LOC) module-by-module.

---

## 5. openhuman (Rust, ~430k LOC, the giant)

**Language:** Rust. Path: `D:/tmp/openhuman/src/`. **Single-crate** workspace (NOT Cargo subcrates like codex) — everything under `src/openhuman/` is one library crate plus binaries.

1. **Module count + size.** `src/openhuman/` has **~50+ top-level modules** (agent, channels, mcp_client, mcp_clients, mcp_server, memory, inference, tokenjuice, billing, composio, security, cost, autocompletion, etc.). Biggest files: `core/observability.rs` 2,949 LOC; `memory/tree/read_rpc.rs` 2,345; `agent/harness/session/turn.rs` 2,202; `config/schema/load.rs` 2,081. Test files routinely 1,500-1,700 LOC.
2. **Boundaries.** Rust `mod`/`pub(crate)` discipline but **single-crate** so all internal modules share the crate boundary. Less hard than codex's per-crate enforcement.
3. **Test coverage shape.** Co-located via `*_test.rs` and `*_tests.rs` sibling files. 192 test files in 1,298 total. Plus separate `e2e/` directory at repo root. Heavy use of `test_support.rs` per module (`agent/harness/test_support.rs` = 1,718 LOC of shared test scaffolding).
4. **LOC discipline.** None. Several 2,000+ LOC files.
5. **Dead-code hygiene.** Not deeply scanned but evidence of duplication: **`mcp_client/` AND `mcp_clients/` AND `mcp_server/` exist as 3 separate top-level modules** (the singular is the old stdio client kept alongside the new pluralized one). Migration debt visible.
6. **Dependency direction.** `core/` is the dispatcher hub (jsonrpc, event_bus, observability). `agent/` is the heaviest consumer, imports from `inference`, `memory`, `mcp_clients`, `composio`, `security`, etc. Dispatcher.rs + dispatcher_tests.rs visible — they tested the dispatch surface explicitly.
7. **Surprised by.** (a) **TokenJuice is its own top-level module** (`tokenjuice/` with classify.rs, reduce.rs, rules/, text/, vendor/, types.rs) — Aura was planning to insert it inline in executor.go; openhuman shows it deserves a sibling module. (b) `mcp_client` (single, stdio-only) AND `mcp_clients` (plural, multi-transport client registry) AND `mcp_server` are all separate concerns. (c) `agent/harness/` contains the entire subagent + session + spawn pattern as a sub-tree under agent — a "harness" is a recognized Rust pattern Aura doesn't use.
8. **Conventions.** `tracing` crate; `anyhow::Result` in app code; per-module `error.rs` for typed errors; `mod.rs` files re-export public surface; `*_test.rs` for unit tests; `*_tests.rs` for the longer integration-style fixture suites; `test_support.rs` for shared scaffolding.

**Aura takeaway:** Openhuman is the **don't-do-this-twice warning**: `mcp_client` + `mcp_clients` + `mcp_server` are 3 modules where one transitional rename was never finished. Also confirms TokenJuice deserves its own top-level module (`internal/tokenjuice/`), NOT inline in the agent loop. The `*/harness/` pattern (everything spawn/session-related lives in `agent/harness/`) is a clean way to bundle Aura's subagent + session-builder + spawn-depth code.

---

## 6. hermes-agent (Python, ~315k LOC, sprawling)

**Language:** Python. Path: `D:/tmp/hermes-agent/`. Flat-ish: **27 top-level directories** at repo root, not nested under a single package.

1. **Module count + size.** Top-level dirs: `agent/` = 102 files / 62,815 LOC; `gateway/` = 62 files / **84,807 LOC** (biggest single file: `gateway/run.py` = **18,207 LOC** — the largest single file in the entire survey); `tools/` = 93 files / 67,386 LOC; `plugins/` = 116 files / 39,406 LOC; `skills/` = 38 files / 13k. `providers/` = 2 files / 375 LOC (just `base.py` + `__init__.py` — the real providers live elsewhere). Biggest agent file: `agent/auxiliary_client.py` = 5,289 LOC; `agent/conversation_loop.py` = **4,094 LOC**.
2. **Boundaries.** Soft, very leaky. `gateway/platforms/discord.py` (5,705 LOC) imports `agent.*` directly. No package-level `__init__.py` policing.
3. **Test coverage shape.** Centralized `tests/` mirror tree with 1,182 test files. Sub-dirs: `tests/agent/`, `tests/gateway/`, `tests/plugins/{browser,memory,model_providers,web,video_gen,image_gen}/`, `tests/e2e/`, `tests/fakes/`. Mocking heavy via `tests/fakes/`. **Highest test-file count of any repo surveyed.**
4. **LOC discipline.** None. `gateway/run.py` is 18,207 LOC. `agent/conversation_loop.py` is 4,094 LOC and the docstring openly admits it was extracted from a 3,900-line method.
5. **Dead-code hygiene.** Only **5 TODO/FIXME in 62k-LOC `agent/`** — extremely low marker density. But the size itself IS debt. Numbered `RELEASE_v0.2.0.md` ... `v0.14.0.md` (13 release notes at repo root) signals incremental sprawl.
6. **Dependency direction.** `agent/` is the spine; `gateway/` consumes it; `plugins/` is an extension surface for tools; `tools/` is the registry. `gateway/platforms/*.py` is where every chat platform lives (discord, telegram, feishu, slack, qqbot, yuanbao, msteams). The 18k-LOC `run.py` violates every layering principle.
7. **Surprised by.** (a) **`gateway/run.py` at 18,207 LOC** — the worst single file in the survey; the project openly acknowledges it. (b) Per-channel **PLATFORM file inside gateway** is 3-5k LOC each (`telegram.py` 5,656, `discord.py` 5,705) — Aura splits per-channel into `outbound/inbound/etc.` files which is healthier. (c) `tools/` and `plugins/` are SEPARATE concerns: tools = built-in registry, plugins = extension surface. Aura conflates these.
8. **Conventions.** Python `logging` module (not loguru); custom `agent.error_classifier.classify_api_error` everywhere; OAuth tokens via custom `_is_oauth_token` checks; `KawaiiSpinner` (display) imported into the agent loop = display tightly bound to logic.

**Aura takeaway:** Hermes is the **don't-do-this warning**: a 4,094-LOC `conversation_loop.py` and an 18,207-LOC `gateway/run.py` are the result of years of layer-by-layer additions with no deep-refactor discipline. They have great test coverage (1,182 files) but the source itself is rotting. **For Aura: never let a single file pass 600 LOC**, no matter how strong the test coverage is.

---

## 7. cli-printing-press (Go, ~134k LOC, audit/lint tooling)

**Language:** Go. Path: `D:/tmp/cli-printing-press/`. **29 internal modules**.

1. **Module count + size.** Biggest: `pipeline/` = 69 files / **50,847 LOC** (worst Go module in survey; `pipeline/scorecard.go` 2,853, `pipeline/dogfood.go` 2,389, `pipeline/live_dogfood_test.go` 2,630). `generator/` = 15 + 70 tests = 30,934 LOC (`generator_test.go` is 10,153 LOC). `cli/` = 30 + 35 = 17,947. `openapi/` = 4 + 7 = 12,694 (`parser_test.go` 5,830, `parser.go` 5,219). `spec/` = 2 + 2 = 7,505. `crowdsniff/` = 6,494. `browsersniff/` = 8,442. Smallest meaningful: `govulncheck/` = 35 LOC. `discovery/` = 85 LOC. `generatedmarker/` = 63 LOC.
2. **Boundaries.** Go `internal/` hard boundary. Inside, sub-packages are flat (no nested directories within most modules). No nested package hierarchy like Aura's `agent/tools/registry/`.
3. **Test coverage shape.** **Co-located `*_test.go`**. 224 tests / 507 total = **44% test files**, second to picobot. Massive test fixtures via `testdata/golden/expected/` (referenced in lefthook glob). Mocking via interfaces + fake servers.
4. **LOC discipline.** **None — but golden files balloon the count.** Many tests are golden-file tests (`testdata/`) that count as test LOC. The `lefthook.yml` pre-commit only runs `gofmt -w`; no LOC cap.
5. **Dead-code hygiene.** **50 TODO/FIXME** across 134k LOC = 1 per 2,700 LOC. Higher density than codex but acceptable. CHANGELOG.md + UPGRADE_LOG.md at root → release discipline visible.
6. **Dependency direction.** `cli/` is the entrypoint, depends on `pipeline/`, which depends on `artifacts/`, `naming/`, `openapi/`, `platform/`, `spec/`. Internal-only `internal_packages.go` file inside `pipeline/` lists allowed deps. Clean layering visible.
7. **Surprised by.** (a) **`mcpdesc/` is 4 files / 858 LOC** — exactly the kind of "compose tool descriptions" helper Aura's MEMORY.md flagged as the lift candidate. Structure: `compose.go` (268), `compose_test.go` (348), `params.go` (135), `params_test.go` (107). Tight + tested. (b) `authdoctor/` = 9 files / 1,251 LOC split into `classify.go` 289, `fingerprint.go` 44, `render.go` 110, `scan.go` 91, `types.go` 58 — **one concern per file under 300 LOC each**. This is the cleanest module shape in the survey. (c) Lefthook runs `gofmt -w` AND auto-syncs `internal/cli/verify_skill_bundled.py` from `scripts/verify-skill/verify_skill.py` — pre-commit hooks enforce **invariants** not just style.
8. **Conventions.** stdlib `log`; explicit `cli.ExitError{Code, Silent}` from `cli/`; `context.Context` first arg; JSON-tag on every exported field; per-module `types.go` for shared structs (authdoctor, openapi, pipeline all have a types.go).

**Aura takeaway:** `authdoctor/` is the **reference for a clean Go module**: 9 files, every concern in its own file (classify / fingerprint / render / scan / types), every file <300 LOC, every source has a sibling `*_test.go`. Apply this shape to every Aura module that grows past 500 LOC: split by concern, one types.go, every file paired with a test. Also adopt `lefthook.yml` for **invariant-style pre-commit hooks** (not just gofmt — also detect drift between two files that must stay synced).

---

## 8. graphify (Python, ~22k LOC, FLAT)

**Language:** Python. Path: `D:/tmp/graphify/graphify/`. **Flat module — every file at the package root**, no subpackages.

1. **Module count + size.** 27 .py files at package root. Biggest: `extract.py` = **6,660 LOC**; `__main__.py` = 3,055 LOC; `callflow_html.py` = 2,014; `export.py` = 1,338; `llm.py` = 1,111; `detect.py` = 1,032. Smallest: `manifest.py` = **4 LOC**; `__init__.py` = 28; `validate.py` = 72.
2. **Boundaries.** No package nesting at all. Imports are flat (`from graphify.extract import ...`). `__init__.py` uses **lazy `__getattr__`** to defer heavy deps until used — clean trick.
3. **Test coverage shape.** Centralized `tests/`, not co-located. Strict per-file test mapping (`tests/test_extract.py`, `tests/test_build.py`, `tests/test_cache.py`, etc.). Fixtures in `tests/fixtures/`.
4. **LOC discipline.** None. `extract.py` is 6,660 LOC in one file — the worst single file after Hermes's `run.py`.
5. **Dead-code hygiene.** Skill-file markdown stubs at the package root (`skill-aider.md`, `skill-codex.md`, ...) — these are **bundled docs**, not dead code, but they live alongside the source which is unusual.
6. **Dependency direction.** Flat means no enforced layering. `__main__.py` is the CLI dispatcher (3,055 LOC) that imports almost everything.
7. **Surprised by.** (a) **Skill bundled docs INSIDE the Python package** (`skill-aider.md`, `skill-claw.md`, etc.) — graphify is itself a skill for many coding agents, so the skill manifest ships in the wheel. Novel pattern. (b) **Lazy `__init__.py` via `__getattr__`** lets the CLI run before heavy deps (tree-sitter, etc.) load — Aura could borrow this for the dashboard SPA. (c) Flat layout for 22k LOC is on the edge — works because each file is a self-contained concern (extract, build, cluster, dedup, export, ingest, llm, report, wiki) but `extract.py` shows the failure mode.
8. **Conventions.** stdlib `logging`; pyproject optional-dependencies per feature (`leiden`, `pdf`, `watch`, `office`); tree-sitter language packs as optional deps; no async (CLI tool).

**Aura takeaway:** Graphify's `__init__.py` lazy `__getattr__` pattern is a clean way to defer heavy imports — applicable to Aura's `internal/storage/sources/` (Mistral OCR client, markitdown client are expensive to wire). But its 6,660-LOC `extract.py` is a warning: **flat layouts encourage god-files**; Aura should keep nested subpackages (`internal/agent/tools/registry/`) but enforce per-file LOC discipline.

---

# CROSS-REPO SYNTHESIS

## Universal patterns (5+ repos share)

1. **Composition root in a thin `main` / entry script.** Every Go/Rust repo has `cmd/<bin>/main.go` (picobot, cli-printing-press) or a similarly thin Rust binary entrypoint that imports + wires modules. Aura's `cmd/aura/app.go` matches this; keep it thin.
2. **Per-module `types.go` / `mod.rs` / `__init__.py` for the shared vocabulary.** CPP's `authdoctor/types.go`, openhuman's per-module `mod.rs` re-exports, codex's `lib.rs` with explicit `pub use`. Aura already does this partially; finish it.
3. **Sibling-file test co-location for Go/Rust; centralized `tests/` mirror tree for Python.** Picobot/CPP/codex/openhuman all sibling tests. Nanobot/hermes/elysia/graphify all centralize. Aura's Go code follows the Go convention — good, don't change.
4. **One LLM/HTTP client interface per provider, mocked via fakes.** Picobot's `providers/provider.go` interface + `openai.go` impl, nanobot's `providers/base.py` + factory, codex's per-provider crates. Aura's `internal/llm/client.go` matches.
5. **`tracing`/`loguru`/`zap` single logging interface, no `println` / `print()`.** Codex's `#![deny(clippy::print_stdout)]` lib lint, openhuman/codex `tracing`, nanobot/elysia `loguru`/`rich`, Aura `zap`. Universal.

## Promising patterns for Aura (2-3 repos use)

1. **API-crate split for plugin contracts** (codex `core-api`, openhuman `mcp_server/protocol.rs`). A ~50-200 LOC interface-only file/crate that plugins depend on instead of full core. Aura should add `internal/agent/contract.go` (and `internal/tools/contract.go`) to break the circular agent↔tools dependency that triggers the "god class" alerts.
2. **"Harness" sub-tree pattern for spawn/session/subagent code** (openhuman `agent/harness/`). Bundles definition/spawn/session/subagent_runner/test_support under one named sub-tree. Aura's swarm + cron + subagent code is currently scattered; pull into `internal/agent/harness/`.
3. **Lefthook + invariant hooks** (CPP `lefthook.yml` syncs `internal/cli/verify_skill_bundled.py` from `scripts/verify-skill/verify_skill.py` automatically). Aura's deep-refactor rule says "every touched module passes golangci-lint + dupl + LOC ≤600"; a pre-commit hook that runs `golangci-lint run` on staged files + checks file LOC would enforce this for free.

## Anti-patterns Aura should NOT adopt

1. **Hermes-style flat top-level dirs at repo root** (`agent/`, `gateway/`, `tools/`, `plugins/`, `providers/`, `cron/`, etc., all at `D:/tmp/hermes-agent/` root level). Result: 18,207-LOC `gateway/run.py` and 4,094-LOC `agent/conversation_loop.py`. Aura's `internal/` boundary prevents this — keep it.
2. **Graphify-style flat package with 27 sibling .py files**. Works at 22k LOC; fails at Aura's 83k. The `extract.py` 6,660-LOC monolith is the predictable outcome.
3. **Codex-style per-crate Cargo subdivision with 60+ crates**. Overkill for Aura's monorepo Go shape; Cargo subcrates have a real edge in Rust (compile parallelism + visibility) that Go's `internal/` packages already give cheaply. Don't split Aura into Go workspaces.
4. **Hermes's "extract a 3,900-line method into a 4,094-line file"** anti-pattern. Splitting a god method into a god file is not refactoring. Aura's deep-refactor rule explicitly forbids this.

## Per-Aura-module reference recommendations

| Aura module | Cleanest curated reference | Why |
|---|---|---|
| `internal/llm/` | **picobot `internal/providers/`** (4 files, 396 LOC, `provider.go` interface + `openai.go` impl + tests) | Tiny + interface-first; Aura already on this shape, just stay disciplined |
| `internal/agent/` | **nanobot `agent/`** (34 files / 12,605 LOC: `loop.py` orchestrator + autocompact/context/hook/memory/runner/subagent capability files) | Pattern Aura is already half-following; complete the split |
| `internal/wiki/` | **graphify** (flat, but with `wiki.py` + `extract.py` + `build.py` + `ingest.py` + `cluster.py` + `analyze.py` as separate concerns) | Graphify's wiki layer = Aura's wiki layer conceptually; lift the file-per-concern split. AVOID graphify's 6,660-LOC `extract.py` failure mode. |
| `internal/storage/search/` | **openhuman `tokenjuice/`** (own top-level module: classify.rs/reduce.rs/rules/text/types.rs) | Confirms search/rerank/dedup deserves its own top-level module, not buried inside `storage/` |
| `internal/channels/` | **picobot `internal/channels/`** (5 files: discord/slack/telegram/whatsapp + stub, all <500 LOC each, `*_test.go` paired) | Aura's per-channel split is right; picobot proves you can stay under 500 LOC per channel. AVOID nanobot's `channels/feishu.py` (1917 LOC) and hermes's `gateway/platforms/telegram.py` (5,656 LOC). |
| `internal/mcp/` | **cli-printing-press `internal/mcpdesc/`** (4 files / 858 LOC: compose.go + compose_test + params.go + params_test) for the **descriptor/compose helper**; openhuman `mcp_server/{protocol.rs, session.rs, stdio.rs, tools.rs}` for the **server side** | Two-layer ref: CPP for tool-description composition (Aura's MEMORY.md already flagged this), openhuman for stdio/protocol split. AVOID openhuman's `mcp_client` + `mcp_clients` duplication — finish the rename in one commit. |

---

# APPENDIX — repo-by-repo single-line takeaways

- **codex** → Use a thin `core-api` interface crate (~80 LOC) to break god-class cycles.
- **elysia** → Centralize shared protocol types in one `objects.py`-equivalent.
- **nanobot** → 1 orchestrator file + ~12 capability files = the right agent/ shape.
- **picobot** → **Size-discipline reference**: 9 modules, all files <500 LOC, 46% test ratio.
- **openhuman** → TokenJuice deserves its own top-level module; never let "mcp_client" + "mcp_clients" coexist past one commit.
- **hermes-agent** → Anti-pattern proof: high test coverage does NOT excuse 18k-LOC files.
- **cli-printing-press** → `authdoctor/` = the cleanest Go module shape (one concern per file, paired tests). Adopt lefthook for invariant enforcement.
- **graphify** → Lazy `__init__.py` is clever; flat 6,660-LOC `extract.py` is not. Nested + disciplined beats flat.
