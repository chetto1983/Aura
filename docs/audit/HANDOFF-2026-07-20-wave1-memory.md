# HANDOFF — Wave 1.10 + memory-tool hardening → next

**Written:** 2026-07-20 (end of the Wave-1 / memory-tools session).
**Read next:** `docs/audit/consolidated-fix-plan-2026-07-20.md` (truth-source), memories `[[aura-fix-plan-wave0-status]]`, `[[aura-l4-archival-memory-state]]`, `[[debug-by-driving-the-agent]]`, `[[web-dist-must-build-on-linux]]`.

---

## 1. Shipped this session (all on `origin/master`, pushed)

| SHA | What |
|---|---|
| `9371e162` | **Cockpit scheduler layout** — the real cause of the "Web-E2E flake" (deterministic, CI on `b6aeabba` failed). SchedulerBoard action buttons collapsed the cron `truncate` span to 0-width → fixed with a compact 44px **icon action rail** + one-truncation-context schedule line. Full CI **green**. |
| `0dcf6029` | Removed the obsolete **windows-unit** CI lane (Windows-native unsupported). |
| `c4504cf8` | **BUG-9 fs_glob/fs_grep** silent-empty — `/root/.cache` (66k files) exhausted the walk budget before user files; prune hidden dot-dirs (+ `scanner.Err()` fix). Operator-found by driving the agent. |
| `c051c64d` | **Wave 1.10** llama.cpp token estimator (DeclaredErrorTokens + `ProviderErrorReserveTokens` in `hardCap()`; retire dead 1.15/256). |
| `18c4ea9d` | Audit doc: recorded the post-removal fixes. |
| `3fb13ede` | **Memory fork batch 1**: exact-text preference **dedup** + **memory_forget** (pref/fact) + **prune** reasoning-trace quartet + memory_export_graph; **system prompt** load-deferred-tool rule + memory-backed `<profile_context>`. |
| `2ddccf26` | **Memory fork batch 2**: **entity forget** (safe, non-cascading) + removed **graph_query** (unscoped exfiltration surface). |

**Memory tool surface now (12, clean):** add_entity, add_fact, add_preference, create_relationship, **forget**, get_context, get_conversation, get_entity, get_facts, list_sessions, search, store_message. Gone: reasoning-trace quartet, export_graph, graph_query.

Fork changes live in the **vendored** copy `docker/agent-memory/` (what the compose builds), NOT re-vendored from `chetto1983/agent-memory` main (which diverges +13 commits / a client refactor). Operator chose "keep the vendored copy, minimal diff". If an upstream PR is wanted it's a separate diff.

## 2. Environment state (as left)

- **Both images redeployed** (`aura:local` + `aura-agent-memory-mcp:local`), healthy. Cockpit `http://localhost:9080`.
- Full stack up (postgres, neo4j, llama-embed, agent-memory-mcp, garage, etc.).
- E2E cockpit creds in `.env` (`AURA_E2E_AUTHULA_EMAIL`/`PASSWORD`). Drive the agent against `:9080` with `AURA_E2E_ORIGIN=http://127.0.0.1:9080 npx playwright test <spec>` (throwaway `_scratch_*.spec.ts` in `web/e2e/`, delete after).
- Web dist must be **Linux-built** (docker webbuild) to pass web-dist-freshness — `[[web-dist-must-build-on-linux]]`.

## 3. ⚠️ OPEN — the PARKED full Agent.md → memory supersession

Operator decision (recorded as a `system`-category Preference in the graph): **"Agent.md deprecated, memory is the source of truth."** Decisions locked this session: **full now**, **start clean** (no Agent.md migration), onboarding does **one LLM call + one batched save**. Batches 1-2 above did the memory-tool + prompt groundwork; the injection/onboarding rewire is NOT done.

**Exact seams (mapped, evidence-cited):**
- Both profile legs converge in `Runner.renderContextBlock` (`internal/runner/runner_context.go:52-87`): leg1 = Agent.md (`r.contextBlock`), leg2 = memory recall (`r.archivalRecaller`, already UNTRUSTED-fenced), leg3 = skills.
- **Drop the Agent.md leg**: `profileContextProvider` (`serve_adapters.go:515-534`) + its injection (`chat_boot.go:346`), `RenderContextBlock`/`ReadProfile` (`internal/profile/`).
- **Flip recall on**: `AURA_CONTEXT_MEMORY_RECALL` default (config) — the memory-poisoning boundary (`[[l4-recall-injection-security-followup]]`); acceptable for the single-operator appliance, formal `/gsd-secure-phase` gates a multi-tenant default.
- **Onboarding → memory**: replace the `ProfileWriter.WriteProfile` seam (web ×3 `onboarding_provision.go:425/442/549`, Telegram ×2 `profile_onboarding.go:259/271`) with a writer that maps `session.Answers` → `memory_add_preference`/`memory_add_fact`. Single-shot extraction (one LLM call for all questions). A batched **`memory_save_profile`** tool was designed (accepts preferences[]+facts[]+entities[]) but NOT built — build it in the fork for the "one save" requirement.
- **Onboarding-complete flag**: the one field with no graph equivalent (`Metadata.OnboardingCompleted`, read by `/api/onboarding/status` `serve_onboarding.go:318-336`) → use a **sentinel memory fact** (`predicate=onboarding_completed`).
- Keep `internal/profile/*` only until the leg is dropped; PRD amendment (operator already ratified in-graph).

## 4. Standing discipline

Real E2E >9.8 not smoke (`[[e2e-real-not-smoke]]`) · **debug by driving the agent**, mine `has_bug` Facts (`[[debug-by-driving-the-agent]]`) · run gates in WSL/container, Windows-native failures are env artifacts (`[[aura-runs-container-ubuntu-only]]`) · never point `db_integration` at live `aura`/neo4j (`[[coverage-gate-nukes-live-db]]`, `[[coverage-gate-nukes-neo4j]]`) · web dist Linux-built (`[[web-dist-must-build-on-linux]]`) · coverage floor 85% on the `db_integration neo4j_integration` matrix · quality-snapshot re-attest at phase close.

## 5. Next candidates

- **Resume the parked supersession** (§3) — the biggest open item.
- **Wave 1** remaining: 1.11 (OpenRouter middle-out fail-safe, pairs with 1.10), then the deadline/liveness cluster (1.1/1.4/1.6/1.3), then 1.2/1.7/1.8-rest/1.5. AG-016 three-tier needs a PRD amendment before code.
- CI: this session's last pushes were pre-push-green; per operator, CI was not re-watched.
