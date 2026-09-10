# The management key as the only OpenRouter credential

Date: 2026-09-10. Status: approved section by section in brainstorming; awaiting review of this text.

## Why

Measured on the local stack on 2026-09-10:

- The admin's Credit panel answers 409 "identity has no OpenRouter key yet"
  (`internal/agui/credit_api.go:135`). `aura.identity_llm_key` holds 0 rows for 2 identities.
  Keys are minted only by the provisioning saga of a NEW identity
  (`internal/agui/onboarding_provision.go:280`), and only when the management key was present
  at daemon boot (`resolveOpenRouterKeyConfig` reads `cfg.OpenRouterManagementKey`,
  `cmd/aura/serve_provisioning_openrouter.go:178`). The admin was bootstrapped on 2026-08-31,
  before compose forwarded the key (de25355e7).
- Web chat and Telegram turns are billed to the deployment `OPENROUTER_API_KEY`:
  `runner.Deps.IdentityLLM` is never assigned outside tests, so spend cannot be split per
  person. Cron agent jobs (`internal/cron/handlers/agentjob.go:139`) and delegations
  (`internal/swarm/swarm_llm_resolve.go:58`) already resolve the identity's own key; Telegram
  uses the resolver only for `/cost`.
- `OPENROUTER_API_KEY` crosses `.env`, compose (`aura`, `aura-ingest`), the installer
  (`packages/create-aura` prompts, `scripts/install.sh`) and the Settings UI.
- `.env.example` carries 240 names. 11 have no reader, 69 equal their compose default in the
  running containers, 54 only feed compose (ports, images, limits). The DB overrides the
  environment only for the ~21 `settings.AllowedKeys`, in the daemon and in
  `aura-media-index` (`settings.OverlayEnv`, `cmd/aura-media-index/main.go:242`). The Python
  ingest reads `AURA_EMBED_DIMENSIONS` and `MULTIMODAL_TIMEOUT_SEC` from the environment.

## Decisions

1. `.env` holds secrets and infrastructure only. Everything else is a code or compose default,
   and every setting editable in the UI lives in `aura.settings`, which always wins.
2. The management key is the only OpenRouter credential an operator enters. Aura mints every
   other key.
3. Route, management key and model are chosen only in the web first-run wizard. The installer
   does infrastructure only and never asks for an OpenRouter key. With Ollama or llama.cpp no
   OpenRouter credential is asked for at all.
4. The admin's key and the services key are minted with no limit. Every other identity is
   minted at a zero cap until an admin tops it up (CRED-02, unchanged).
5. One idempotent reconciler mints every missing key.
6. The clean reinstall on this PC wipes all state after a file backup of the ArcadeDB memory,
   and keeps the model and package caches.

## Design

### Credentials

- **Management key.** The `aura.settings` row `AURA_OPENROUTER_MANAGEMENT_KEY` (secret),
  entered in the wizard or in Settings. It is read from the settings store at call time. The
  four boot-time captures — the credit API and the spend overview (`cmd/aura/serve_agui.go:257`,
  `:286`), the mint and revoke adapters (`cmd/aura/serve_provisioning_openrouter.go:149`,
  `:160`) — are always wired and fail at call time with a "management key not set" error when
  the row is empty. Saving the key needs no restart.
- **Services key.** The `aura.settings` row `OPENROUTER_API_KEY`, minted by Aura under the name
  `aura-services` with no limit, never typed by a person. It feeds the process-wide runtime:
  `aura-media-index` OCR and vision (which already overlays `aura.settings`), cloud embeddings,
  TTS and STT. A row that already exists (an older install) is left in place. The key stays in
  `settings.AllowedKeys`, because that is what lets `OverlayEnv` apply it; only the Settings UI
  stops rendering it.
- **Per-identity keys.** `aura.identity_llm_key` as today: AES-256-GCM ciphertext, RLS,
  `external_user` set to the identity id. An identity is an admin when it holds
  `identity.CapIdentityCreate` (`identity.Administrative()`), the capability `/api/me` turns
  into `isAdmin`. An identity is active when it is not deactivated (`Identity.Deactivated`,
  the flag `RequireAuth` already checks).

### The reconciler: `EnsureOpenRouterKeys(ctx)`

- On a local route (the base URL `allowsKeylessLocalLLMBaseURL` classifies as keyless) it does
  nothing.
- With the management key unset it does nothing and logs once.
- Otherwise, under one advisory lock, because boot and a settings save can race:
  - mint the services key when its row is empty, and write it through the settings PUT path
    (`settings.Store.ReplaceMany` plus `primaryLLMRouteReloader.Prepare`), so the runtime is
    republished without a restart;
  - for every active identity with no stored key, mint and persist one: no limit for an admin,
    a zero cap with a monthly reset for everyone else. A persist that fails revokes the key at
    the provider (the existing T-02-31 pattern).
- Triggers: saving the management key or switching the route (settings PUT), daemon boot, and
  the new-identity saga, whose `openrouter_key` step keeps its journal and its compensation but
  mints through the same per-identity function.
- One identity that fails does not stop the others. The call returns the joined error, and the
  Credit panel shows that identity's no-key state.
- The seeded `aura-cli` identity is an identity like any other: it gets a zero-cap key unless
  it holds `identity.CapIdentityCreate`.

### Turns

`cmd/aura/chat_boot.go:487` passes `buildIdentityLLMResolver(chat)` as `runner.Deps.IdentityLLM`,
so web and Telegram turns are billed to the identity's own key. A missing key refuses the turn;
nothing falls back to the services key (CRED-07). The local route stays exempt (D-13).

### "No limit"

- The next free migration (0125 today; the number is read from `ls internal/db/migrations` at
  landing) makes `aura.identity_llm_key.limit_usd` nullable, NULL meaning no limit. The down
  migration sets NULL rows to 0 and restores `NOT NULL DEFAULT 0`.
- `identitykey.Record.LimitUSD` becomes `*float64`. `openrouterprovision.MintRequest.Limit`
  becomes `*USDCap`, and nil is sent as `"limit": null`. A pointer does not have the
  `omitempty` trap the value type has.
- `identitykey.Decide`: a nil limit allows; a limit ≤ 0 still refuses with no credit.
- The Credit panel shows "no limit" and can set a cap or clear it (PATCH with `limit: null`).

### First-run wizard

- The model step becomes the route step, the only step that cannot be skipped. OpenRouter asks
  for the management key (required) and the model; Ollama and llama.cpp ask for the base URL and
  the model.
- Saving runs the reconciler. The step then shows the masked labels of the admin key and the
  services key, or the provider's error, before moving on.
- `OPENROUTER_API_KEY` leaves `PRIMARY_SETTINGS`: nobody edits it anymore.
- The Credit panel renders a 409 as "this identity has no key yet", with the cause, instead of
  the generic load error.

### Installer, compose, `.env`

- `create-aura` drops the route, model and key prompts. `install.conf` moves to `format=2`
  without `llm_provider`, `llm_base_url`, `llm_model` and `openrouter_api_key`. `install.sh`
  drops `apply_install_config` and the `OPENROUTER_API_KEY` line of its template. The template
  keeps the 13 secrets compose requires plus the values that differ from compose defaults
  (`AURA_PROFILE`, `AURA_MUSR_ISOLATION`, `AURA_SANDBOX_IMAGE`, `COMPOSE_PROFILES`, `AURA_IMAGE`,
  the embedding model path and URL).
- `compose.yaml` stops passing `OPENROUTER_API_KEY`, `AURA_OPENROUTER_MANAGEMENT_KEY` and
  `AURA_LLM_PROVIDER/MODEL/BASE_URL` to `aura` and `aura-ingest`, and `TELEGRAM_BOT_TOKEN` to
  `aura`. The installer payload manifest is regenerated.
- `AURA_EMBED_DIMENSIONS` leaves `settings.AllowedKeys` and the Settings UI. The embedding model
  file fixes it, and changing it breaks the vector index. It stays an environment default read by
  the daemon and the Python ingest.
- The messages that tell the operator to "set OPENROUTER_API_KEY in .env"
  (`cmd/aura/llm_client.go:16`, `internal/llm/config.go:19`) point at the first-run wizard.
- `.env.example` keeps the required secrets and the documented infrastructure knobs. The 11
  names with no reader and the names equal to their compose default go.
- A test fails when `.env.example` gains a name that no code, compose file or script reads, or a
  value equal to its compose default.

## Testing

- **Unit.** The reconciler: idempotent; no-op on a local route and with no management key; no
  limit for an admin and zero for everyone else; the services key written through the hot path;
  one failed identity does not stop the rest. `Decide` with a nil limit. `MintRequest` with a nil
  limit marshals `null`. The wizard route step: required, and the management key required on
  OpenRouter. The Credit panel's no-key state and no-limit rendering. The installer's `format=2`.
  The `.env.example` guard.
- **Integration (`db_integration`).** The migration up and down. `identitykey` Save and Load with
  a NULL limit under RLS.
- **E2E on this PC after the clean reinstall (Definition of Done).** A fresh install through the
  package, the first operator, the wizard with route OpenRouter and the management key. Then:
  `GET /api/v1/keys` shows the admin's key (name = identity id, `external_user` set, `limit`
  null) and `aura-services` (`limit` null); an admin chat turn is billed to the admin's key
  (analytics `api_key_id`); the spend page lists the admin; the Credit panel shows "no limit"; a
  second identity is minted at zero and refused until topped up.

## Reinstall on this PC

1. Back up ArcadeDB to a file kept outside the volumes: the `aura-memory` MCP that Claude and
   Codex share lives there.
2. `docker compose down`, then remove every `aura_*` volume except the model and package caches
   (`aura-llama-embed`, `aura-llm`, `aura-ocr-vl`, `aura-npm-cache-host`, `aura-pip-cache-host`,
   `aura-uv-cache-host`); remove `~/.aura` and `.env`.
3. Install with the new package and run the E2E above.

## Not covered

- Appliances that are already installed keep their `.env` and any `OPENROUTER_API_KEY` row they
  hold; the reconciler leaves that row in place. How a new `compose.yaml` reaches them is the
  open question already recorded for de25355e7.
- Host CLI commands that read `OPENROUTER_API_KEY` from `.env` through godotenv lose it. They
  must overlay `aura.settings` the way `aura-media-index` does, or run inside the container.
