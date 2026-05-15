# Settings / Secrets read-vs-write audit — 2026-05-15

Scope: every key in `internal/api/settings.go::settingsCatalog` (the dashboard
registry, lines 81-122). For each key we trace:

- **Dashboard write target** — what table `handleSettingsUpdate`
  (`internal/api/settings.go:333-390`) actually writes to.
- **Boot read source** — which overlay populates `cfg.<Field>` for the running
  process, given the boot ordering at `cmd/aura/main.go:265-313`:
  1. env → `config.Load()`
  2. settings overlay → `config.ApplyToConfig` (settings table)
  3. **secrets overlay → `applySecretsToConfig` (secrets table) — LAST WRITE WINS**
- **Wizard write target** — where the first-run wizard
  (`internal/api/setup_server.go:127-193`) writes the same value.

## Findings

`handleSettingsUpdate` is uniform: every accepted key is written via
`deps.Settings.Set(ctx, k, v)` (settings.go:366). `deps.Settings` is the
`config.Writer` for the **settings** table — it has NO branch on `IsSecret`,
and there is no code path in `internal/api/settings.go` that touches the
`secrets.Store` at all (grep-confirmed: the only `secrets` import-driven write
in `internal/api/` is the wizard at `setup_server.go:154,164` plus the helper
`setup_dotenv.go:99`).

`applySecretsToConfig` overlays exactly 6 fields, AFTER `ApplyToConfig`, so for
those 6 fields **the secrets table is authoritative at boot** whenever a row
exists in it — even if the user just rewrote the equivalent settings row from
the dashboard.

## Per-key table

| Setting key (as in registry) | IsSecret | Dashboard write target | Boot read source | Wizard write target | Mismatch? | Why |
|---|---|---|---|---|---|---|

| `AURA_TIMEZONE` (KeyTimezone) | no | settings table (settings.go:366 → `deps.Settings.Set`) | settings table (applier.go:156 `cfg.Timezone = settingString(... KeyTimezone ...)`) | n/a | NO | single source: settings table |
| `LLM_BASE_URL` (KeyLLMBaseURL) | no | settings table (settings.go:366) | settings table (applier.go:181) | settings table (setup_server.go:172 `config.KeyLLMBaseURL`) | NO | single source: settings table |
| `LLM_MODEL` (KeyLLMModel) | no | settings table (settings.go:366) | settings table (applier.go:182) | settings table (setup_server.go:173 `config.KeyLLMModel`) | NO | single source: settings table |
| `LLM_API_KEY` (KeyLLMAPIKey) | YES | **settings table** (settings.go:366, no secret-branch) | **secrets table** (secrets_boot.go:24 `override(secrets.KeyLLMAPIKey, &cfg.LLMAPIKey)` runs AFTER applier.go:180) | **secrets table** (setup_server.go:160 `secrets.KeyLLMAPIKey` via `writeSecret`) | **BROKEN** | dashboard writes to settings.`LLM_API_KEY` but boot prefers secrets.`llm_api_key` (lowercase, different table). Wizard primes secrets table on first install, so dashboard edits silently lose to stale wizard value. **This is the 401.** |
| `LLM_MAX_RETRIES` (KeyLLMMaxRetries) | no | settings table (settings.go:366) | settings table (applier.go:183) | n/a | NO | single source |
| `MAX_CONTEXT_TOKENS` (KeyMaxContextTokens) | no | settings table | settings table (applier.go:169) | n/a | NO | single source |
| `MAX_HISTORY_MESSAGES` (KeyMaxHistoryMessages) | no | settings table | settings table (applier.go:170) | n/a | NO | single source |
| `SOFT_BUDGET` (KeySoftBudget) | no | settings table | settings table (applier.go:171) | n/a | NO | single source |
| `HARD_BUDGET` (KeyHardBudget) | no | settings table | settings table (applier.go:172) | n/a | NO | single source |
| `COST_INPUT_PER_M_TOKENS` (KeyCostInputPerMTokens) | no | settings table | settings table (applier.go:173-175) | n/a | NO | single source |
| `COST_OUTPUT_PER_M_TOKENS` (KeyCostOutputPerMTokens) | no | settings table | settings table (applier.go:176-178) | n/a | NO | single source |
| `AURA_PROMPT_VERSION` (KeyPromptVersion) | no | settings table | settings table (applier.go:213) | n/a | NO | single source |
| `AURA_SKILL_ROUTING_MODE` (KeySkillRoutingMode) | no | settings table | settings table (applier.go:214) | n/a | NO | single source |
| `AURA_AGENT_LOOP_MAX_STEPS` (KeyAgentLoopMaxSteps) | no | settings table | settings table (applier.go:215) | n/a | NO | single source |
| `AURA_REASONING_EFFORT` (KeyReasoningEffort) | no | settings table | settings table (applier.go:216) | n/a | NO | single source |
| `TOOL_SEARCH_TOP_K` (KeyToolSearchTopK) | no | settings table | settings table (applier.go:217) | n/a | NO | single source |
| `TOOL_SEARCH_BACKEND` (KeyToolSearchBackend) | no | settings table | settings table (applier.go:218) | n/a | NO | single source |
| `MAX_TOOL_RESULT_CHARS` (KeyMaxToolResultChars) | no | settings table | settings table (applier.go:219) | n/a | NO | single source |
| `MICROCOMPACT_KEEP_RECENT` (KeyMicrocompactKeepRecent) | no | settings table | settings table (applier.go:220) | n/a | NO | single source |
| `MICROCOMPACT_MIN_CHARS` (KeyMicrocompactMinChars) | no | settings table | settings table (applier.go:221) | n/a | NO | single source |
| `AURA_TERMINAL_TOOL_POLICY` (KeyTerminalToolPolicy) | no | settings table | settings table (applier.go:222) | n/a | NO | single source |
| `AURA_DELEGATION_MODE` (KeyDelegationMode) | no | settings table | settings table (applier.go:223) | n/a | NO | single source |
| `SKILLS_ADMIN` (KeySkillsAdmin) | no | settings table | settings table (applier.go:202) | n/a | NO | single source |
| `WEB_SEARCH_PROVIDER` (KeyWebSearchProvider) | no | settings table | settings table (applier.go:185) | n/a | NO | single source |
| `SEARXNG_BASE_URL` (KeySearXNGBaseURL) | no | settings table | settings table (applier.go:186) | n/a | NO | single source |
| `QDRANT_URL` (KeyQdrantURL) | no | settings table | settings table (applier.go:196) | n/a | NO | single source |
| `QDRANT_COLLECTION` (KeyQdrantCollection) | no | settings table | settings table (applier.go:197) | n/a | NO | single source |
| `QDRANT_API_KEY` (KeyQdrantAPIKey) | YES | **settings table** (settings.go:366) | **secrets table** (secrets_boot.go:28 `override(secrets.KeyQdrantAPIKey, &cfg.QdrantAPIKey)`) | not written (wizard doesn't ask for Qdrant key) | **BROKEN** | same shape as LLM_API_KEY: uppercase vs lowercase, settings vs secrets table. Only the `.env`-migration path (migrate.go:23 `"QDRANT_API_KEY": KeyQdrantAPIKey`) ever populates the secrets row; once it exists, dashboard edits are silently dropped at next boot. |
| `EMBEDDING_BASE_URL` (KeyEmbeddingBaseURL) | no | settings table | settings table (applier.go:210) | n/a | NO | single source |
| `EMBEDDING_MODEL` (KeyEmbeddingModel) | no | settings table | settings table (applier.go:211) | n/a | NO | single source |
| `EMBEDDING_API_KEY` (KeyEmbeddingAPIKey) | YES | **settings table** (settings.go:366) | **secrets table** (secrets_boot.go:25 `override(secrets.KeyEmbeddingAPIKey, &cfg.EmbeddingAPIKey)`) | **secrets table** (setup_server.go:161 `secrets.KeyEmbeddingAPIKey`) | **BROKEN** | same shape as LLM_API_KEY. Wizard primes secrets row; dashboard later writes settings row; boot reads secrets row. |
| `EMBEDDING_OUTPUT_DIM` (KeyEmbeddingOutputDim) | no | settings table | settings table (applier.go:212) | n/a | NO | single source |
| `MISTRAL_API_KEY` (KeyMistralAPIKey) | YES | settings table (settings.go:366) | **settings table** (applier.go:228 — NOT overlaid by `applySecretsToConfig`) | settings table (setup_server.go:174 `config.KeyMistralAPIKey`) | NO | flagged `IsSecret: true` in the registry, but `applySecretsToConfig` does NOT override it (secrets_boot.go:15-29) and `secrets.envVarToSecretKey` (migrate.go:17-24) has no entry for it. So write→settings and read→settings: consistent. The "secret" classification is for UI redaction only; storage path is single-table. |

## Summary

### BROKEN keys (3)

These three are the 401 root cause and any future 401 on the embedding sidecar
or Qdrant:

1. **`LLM_API_KEY`** — dashboard writes to settings.`LLM_API_KEY`; boot
   `applySecretsToConfig` overwrites `cfg.LLMAPIKey` from secrets.`llm_api_key`
   (which the wizard populated at install time and never gets cleared).
   Result: dashboard rotate-key flow silently no-ops.
2. **`EMBEDDING_API_KEY`** — identical bug shape. Wizard writes
   secrets.`embedding_api_key` (setup_server.go:161); dashboard writes
   settings.`EMBEDDING_API_KEY` (settings.go:366); boot reads secrets row last
   (secrets_boot.go:25).
3. **`QDRANT_API_KEY`** — same shape, but no wizard prime — the secrets row
   only exists if `.env` had `QDRANT_API_KEY=...` at first boot (migrate.go:23).
   When it does exist, dashboard edits are silently dropped.

The 3 other secrets in the secrets table (`telegram_token`,
`garage_s3_access_key`, `garage_s3_secret_key`) are not in the dashboard
registry — they're written by the wizard / .env migration only and have no
dashboard write path, so they don't surface as mismatches in this audit. They
remain single-source (secrets table).

### OK keys (31)

All non-`IsSecret` keys, plus `MISTRAL_API_KEY` (which is `IsSecret` for UI
redaction but stored in the settings table only — `applySecretsToConfig`
ignores it). Single-source, single-table, write and read aligned.

### ORPHAN / DUAL_WRITE flags

- **No ORPHAN keys.** Every registry entry has a write handler via
  `handleSettingsUpdate` + `config.IsOverridable`.
- **DUAL_WRITE risk on the 3 BROKEN keys.** Today they are not strictly
  dual-written by one user action, but the system overall has both a wizard
  path (→ secrets table) and a dashboard path (→ settings table) writing the
  same logical secret to different tables. This is the latent race that surfaces
  as 401.

## Minimum-LOC fixes (1 sentence each)

Pick **one** of these — they fix all 3 BROKEN keys together. The user asked
"is the fix scoped to LLM_API_KEY or to all secret-shaped keys" → **all
secret-shaped keys at once**, because the 3 broken keys share one bug shape
(`applySecretsToConfig` is the single point that violates the contract).

1. **Preferred (smallest LOC, preserves the H04 "no secrets in settings table"
   intent):** In `internal/api/settings.go::handleSettingsUpdate`, branch on a
   secret-key map: for `KeyLLMAPIKey`, `KeyEmbeddingAPIKey`, `KeyQdrantAPIKey`
   (and any future `IsSecret: true` registry entry) call
   `deps.SecretsStore.Set(ctx, secrets.Key<X>, v)` instead of
   `deps.Settings.Set(ctx, k, v)`. Requires adding `SecretsStore secrets.Store`
   to `Deps`. ~10-15 LOC.
2. **Alternative (zero new wiring, less hygienic):** Remove
   `applySecretsToConfig`'s override of `cfg.LLMAPIKey`, `cfg.EmbeddingAPIKey`,
   `cfg.QdrantAPIKey` (secrets_boot.go:24,25,28) and rely on settings-table
   overlay only for those three; keep secrets overlay for
   `telegram_token`/`garage_*` which have no dashboard write path. Reverts part
   of US-H03 for the 3 keys the dashboard rotates. ~3 LOC delete.
3. **Alternative (cheapest if you also want `GET /settings` to read the right
   place):** Make `currentValueFor` / `handleSettingsList` also read from the
   secrets store for `IsSecret` registry keys, and make
   `handleSettingsUpdate` write there too — i.e. fully route secret keys
   through `secrets.Store` on both read and write. ~20-25 LOC, matches Phase-H
   intent cleanly.

## File:line citations

- Dashboard write path: `internal/api/settings.go:333-390` (single
  `deps.Settings.Set` call at line 366).
- Settings overlay (boot step 2): `internal/config/applier.go:142-241`, with
  the 3 broken-key overlays at lines 180, 198, 209.
- Secrets overlay (boot step 3, LAST WIN): `cmd/aura/secrets_boot.go:15-29`,
  6 overrides at lines 23-28.
- Secret key constants (lowercase): `internal/secrets/store.go:17-24`.
- `.env` → secrets migration map: `internal/secrets/migrate.go:17-24`.
- Wizard secret writes: `internal/api/setup_server.go:154` (telegram_token),
  `setup_server.go:159-167` (llm_api_key, embedding_api_key).
- Wizard settings writes: `internal/api/setup_server.go:171-184`
  (LLM_BASE_URL, LLM_MODEL, MISTRAL_API_KEY).
- Boot ordering: `cmd/aura/main.go:265-313` — settings overlay at line 270,
  secrets overlay at line 280; post-wizard re-load applies same order at lines
  309 → 312.
- `MISTRAL_API_KEY` is **not** in `applySecretsToConfig`, so it stays in
  settings table — secrets_boot.go:15-29 omits it.
