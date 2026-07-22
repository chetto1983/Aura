# HANDOFF — 2026-07-23: Aura fully local (Qwythos) + gauge baco + fix-plan Wave 1 start

**Written:** end of the 2026-07-22/23 session. All work below is on `origin/master` (pushed, hooks green).

## TL;DR — what shipped this session
1. **Aura runs FULLY LOCAL** on **Qwythos-9B-v2** (llama.cpp CUDA, RTX 3060) — zero OpenRouter. Tool-calling + reasoning verified live.
2. **Fix-plan 1.1 (Wallclock finalize DOA)** — code + review Approved (`ac636ddc`) + **E2E-validated on the local model**.
3. **Cockpit context-gauge baco** — the footer hardcoded a 1M window; now threads the live model window (`80108105`), live-verified.
4. Dead-code `MutatingOperationMetadata` removed (`f7622147`).

Commits (all pushed): `ac636ddc` (1.1), `f7622147` (deadcode), `6e15bc44` (compose-llm), `80108105` (gauge). Later auto-synced/pushed.

## Aura is now LOCAL — how it's wired (IMPORTANT)
- **Model server:** `aura-llm` (compose.llm.yaml) = `ghcr.io/ggml-org/llama.cpp:server-cuda`, GPU, port 8084, `--jinja` (Qwen3.5 tool-calling), `--reasoning-format deepseek` (surfaces `<think>`). Model **Qwythos-9B-v2-MTP-Q4_K_M** (Qwen3.5-based hybrid Gated-DeltaNet SSM, multimodal — mmproj loaded), cached in the `aura-llm` volume.
- **Bring up:** `AURA_LLM_HF_REPO=empero-ai/Qwythos-9B-v2-GGUF AURA_LLM_HF_FILE=Qwythos-9B-v2-MTP-Q4_K_M.gguf AURA_LLM_ALIAS=qwythos-9b docker compose -f compose.yaml -f compose.llm.yaml up -d aura-llm aura`
- **Routing switch is in the DB** (`aura.settings`, DB wins over env via OverlayEnv at boot; the cockpit "Locale" toggle writes the same rows). Current rows: `AURA_LLM_PROVIDER=llamacpp`, `AURA_LLM_BASE_URL=http://aura-llm:8084/v1`, `AURA_LLM_MODEL=qwythos-9b`, `AURA_MODEL_CONTEXT_WINDOW=131072`. **Changing a setting needs an `aura` restart.**
- **Revert to OpenRouter:** delete/update those `aura.settings` rows (or use the cockpit "Cloud" toggle) + restart `aura`.
- **1M context does NOT fit the 3060 12GB** — MEASURED: 256K saturates VRAM (132MB free), 1M needs ~16-20GB KV → OOM (even the hybrid small-KV arch). **128K is the ceiling here.** compose.llm.yaml default is now 128K + `-np 1` + YaRN (`--rope-scaling yarn --yarn-orig-ctx 16384`); bump `AURA_LLM_CTX` only on bigger VRAM (DGX Spark). `HF_HOME=/root/.cache/llama.cpp` (the volume) so the ~6GB GGUF survives recreates — WITHOUT this, every `--force-recreate` re-downloads.
- Speed: ~46 tok/s gen on the 3060. `AURA_MODEL_CONTEXT_WINDOW=131072` must match the model — a mismatch (e.g. 1M) would let Aura send prompts the model truncates.

## fix-plan Wave 1 — status
Source of truth: `docs/audit/consolidated-fix-plan-2026-07-20.md` (Wave 1 table). Executing subagent-driven, one item at a time.
- **1.1 Wallclock finalize DOA — DONE** (`ac636ddc`, on origin). Premise confirmed: `internal/runner/runner.go:512` `bud.WithDeadline(ctx)` gives `ic.Ctx` the wallclock deadline, so on a wallclock trip the salvage synthesis/critic derived `WithTimeout(ic.Ctx, …)` was DOA. Fix: `context.WithoutCancel(ic.Ctx)` for synthesis (finalize.go), critic (completion.go), and the recovery turn only (llm_agent.go — `recoveryTurn := skipBudgetGate`); the normal loop call (llm_agent.go:251) is unchanged. **E2E validated on the local model**: with `AURA_LOOP_MAX_WALLCLOCK_SEC=15`, a 500-word-essay turn tripped the deadline (`stream_deadline: context deadline exceeded` in the log) yet returned a complete 116KB answer — pre-fix it would have been empty/DOA.
- **PENDING (next):** 1.4 `/readyz` scheduler tick-freshness probe · 1.5 ingestion silent-drop observability · 1.3 SSE heartbeat + reconnect · 1.6 MCP HTTP reconnect-on-use + ping poll · 1.11 OpenRouter middle-out fail-safe (**now less relevant — we're local, not OpenRouter**). Also open: 1.2/1.7/1.8/AG-016 need PRD/migration first (see the fix-plan).

## GOTCHAS (bit me this session — don't repeat)
- **CLI `aura chat` fails here:** `resolve owner identity: get identity "local": identity not found` — this web deploy has NO "local" identity (operator is `b130c94d-a213-463a-a797-ec124104363a`). **Drive E2E via the cockpit**, not the CLI.
- **Cockpit E2E recipe** (see `[[cockpit-e2e-idempotency-key]]` + scratchpad `e2e_docindex.sh`/`e2e_wallclock.sh`): login (csrf → `/auth/email-password/sign-in` double-submit) → `POST /api/conversations` **+ `Idempotency-Key` header** → `POST /agent/run` **+ `Idempotency-Key`** `{threadId, messages:[{id,role,content}]}` SSE. **Body must be clean JSON** — an unescaped apostrophe in `content` triggered `{"error":"invalid mutation request body"}` from the idempotency middleware. SSE `delta`s are token-split (reassemble before asserting). Creds in `.env` (`AURA_E2E_AUTHULA_EMAIL`/`_PASSWORD`, trim trailing spaces).
- **Wallclock-trip E2E:** `AURA_LOOP_MAX_WALLCLOCK_SEC` is NOT a settings-allowlist key and NOT in compose — use a temp compose override (`services.aura.environment.AURA_LOOP_MAX_WALLCLOCK_SEC`) + `--force-recreate`, then restore. Delete the temp override file (don't commit it).
- **DO NOT run the docker webbuild with the host `web/` bind-mounted** to just check the dist — it overwrites `web/node_modules` with **Linux** binaries and the **Windows** pre-push `web`/`web-deadcode` hooks then fail (eslint not runnable). Recovery: `cd web && npm ci` on Windows (PowerShell). To verify the committed dist is a Linux build, compare after a docker build — this session it matched byte-for-byte (0 diff), so the implementer's dist was already a correct Linux build.
- **LSP diagnostics are frequently STALE** (hit 4×: `undefined DocumentIndex`, unused `strings`, `DuplicateDecl`, `contextWindow undefined`) — always confirm with a real `go build`/`go test`, never trust the red squiggles.
- `cmd/aura/serve.go` is at exactly **600 LOC** — split on next touch.

## Stack state at handoff
- Full stack up; `aura` on Qwythos local @128K, healthy, wallclock restored to default 300s. Working tree clean.
- `AURA_MUSR_ISOLATION` still OFF with 26 identities (documents plane unscoped) — pre-existing Wave 3 item, not this session.

## Next session — resume at
1. Continue fix-plan **Wave 1** subagent-driven: **1.4** (`/readyz` scheduler probe) is the cleanest next (low risk, no PRD/migration). Then 1.5, 1.3, 1.6. Skip/defer 1.11 (OpenRouter-only).
2. Ledger: `.superpowers/sdd/progress.md`.
