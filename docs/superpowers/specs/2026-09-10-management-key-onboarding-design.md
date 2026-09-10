# The management key as the only OpenRouter credential

Date: 2026-09-10. Status: approved section by section in brainstorming, then revised the same
day after an adversarial review of the first draft (3e1c9f6ad). Awaiting review of this text.

## Why

Measured on the local stack on 2026-09-10:

- The admin's Credit panel answers 409 "identity has no OpenRouter key yet"
  (`internal/agui/credit_api.go:135`). `aura.identity_llm_key` holds 0 rows for 2 identities.
  Keys are minted only by the provisioning saga of a NEW identity
  (`internal/agui/onboarding_provision.go:280`), and only when the management key was present
  at daemon boot (`cmd/aura/serve_provisioning_openrouter.go:178`). The admin was bootstrapped
  on 2026-08-31, before compose forwarded the key (de25355e7).
- Web chat and Telegram turns are billed to the deployment `OPENROUTER_API_KEY`:
  `runner.Deps.IdentityLLM` is never assigned outside tests, so spend cannot be split per
  person. Cron agent jobs (`internal/cron/handlers/agentjob.go:139`) and delegations
  (`internal/swarm/swarm_llm_resolve.go:58`) already resolve the identity's own key; Telegram
  uses the resolver only for `/cost`.
- `OPENROUTER_API_KEY` crosses `.env`, compose (`aura`, `aura-ingest`), the installer
  (`packages/create-aura` prompts, `scripts/install.sh`) and the Settings UI.
- `.env.example` carries 240 distinct names. 11 have no reader, 69 equal their compose default
  in the running containers, 54 only feed compose (ports, images, limits). The DB overrides the
  environment only for the ~21 `settings.AllowedKeys`, in the daemon and in `aura-media-index`
  (`settings.OverlayEnv`, `cmd/aura-media-index/main.go:242`).

The adversarial review established, and this revision relies on:

- The per-identity resolver is built from the boot config and caches its clients
  (`cmd/aura/serve_delegation.go:53`, `internal/runner/runner_identity_llm.go:128,135-138`). It
  would not follow a route or model chosen in the wizard.
- `primaryLLMRouteReloader.Prepare` resets provider, base URL, model and API key to the boot
  fallback and re-applies only the overrides it receives (`cmd/aura/serve_settings.go:108-122`).
  The settings handlers hold `settingsMu` (`internal/agui/settings_api.go:254,340`).
- Cloud embeddings, TTS/STT and daemon-side vision read `cfg.LLM.APIKey` once at boot.
- Every identity holds `governance.write` (`internal/identity/capabilities.go:45`), and that is
  the only check on the settings PUT and DELETE routes (`cmd/aura/serve_webui.go:212-214`).
- `OverlayEnv` puts every allowlisted row into the process environment
  (`internal/settings/settings.go:221-226`), and the skills installer hands the whole
  environment to `npx skills add` (`internal/skills/installer.go:378`). `aura.settings` stores
  values in plaintext.
- `patchRequestWire.Limit` is `*USDCap` with `omitempty` (`internal/openrouterprovision/wire.go:278`),
  so a PATCH cannot send `"limit": null`.
- The first-run wizard is a dismissible overlay (`web/src/AppShell.tsx:567-571`).

## Decisions

1. `.env` holds secrets and infrastructure only. Everything else is a code or compose default,
   and every setting editable in the UI lives in `aura.settings`, which always wins.
2. The management key is the only OpenRouter credential an operator enters. Aura mints every
   other key.
3. Route, management key and model are chosen only in the web first-run wizard, only by an
   admin. The installer does infrastructure only and never asks for an OpenRouter key. With
   Ollama or llama.cpp no OpenRouter credential is asked for at all.
4. The admin's key is minted with no limit. The services key gets a monthly cap chosen in the
   wizard. Every other identity is minted at a zero cap until an admin tops it up (CRED-02,
   unchanged).
5. One idempotent reconciler mints every missing key.
6. Only an admin can change the management key, the route or the model.
7. Secret settings leave the process environment and are encrypted at rest.
8. The wizard restarts Aura once, after the first services key is minted.
9. The clean reinstall on this PC wipes all state after a file backup of the ArcadeDB memory,
   and keeps the model and package caches.

## Design

### Credentials

- **Management key.** The `aura.settings` row `AURA_OPENROUTER_MANAGEMENT_KEY`, read from the
  settings store at call time. The five boot-time captures — the credit API, its
  `creditBackendBills` and the spend overview (`cmd/aura/serve_agui.go:251,257,286`), the mint
  and revoke adapters (`cmd/aura/serve_provisioning_openrouter.go:149,160`) — are always wired
  and decide at call time, failing with "management key not set" when the row is empty.
- **Services key.** The `aura.settings` row `OPENROUTER_API_KEY`, minted by Aura under the name
  `aura-services` with the monthly cap the admin chose, never typed by a person. It feeds the
  process-wide runtime: `aura-media-index` OCR and vision, cloud embeddings, TTS and STT. An
  existing row from an older install is left in place. The settings API refuses to write it;
  replacing it revokes the previous key at the provider.
- **Per-identity keys.** `aura.identity_llm_key` as today: AES-256-GCM ciphertext, RLS,
  `external_user` set to the identity id. An identity is an admin when it holds
  `identity.CapIdentityCreate`, the capability `/api/me` turns into `isAdmin`. An identity is
  active when it is not deactivated (`Identity.Deactivated`). Service identities
  (`kind = 'service'`, such as the seeded `aura-cli`) get no key; the ingest supervisor already
  skips them the same way.

### Secret settings

- Every `settings.AllowedKeys` row with `Secret: true` (`OPENROUTER_API_KEY`,
  `AURA_OPENROUTER_MANAGEMENT_KEY`, `TELEGRAM_BOT_TOKEN`) is stored as AES-256-GCM ciphertext
  under a key derived by HKDF from `AURA_AUTHULA_SECRET`, with its own info string
  `aura-settings-secret-v1` (domain-separated from `identitykey`'s `aura-identity-llm-key-v1`).
  The ciphertext stays in the existing `value` column behind a version prefix
  (`enc:v1:<nonce hex>:<ciphertext hex>`), so `aura.settings` needs no migration. At boot the
  daemon encrypts any secret row that lacks the prefix, so an upgraded install converges.
  LibreChat stores its admin-config secrets the same way (`v3:<iv>:<ciphertext>`,
  `packages/api/src/admin/secrets.ts:6-7` in v0.8.8-rc2), but it keys them with two extra
  `.env` secrets, `CREDS_KEY` and `CREDS_IV`; deriving from `AURA_AUTHULA_SECRET` adds none.
- `OverlayEnv` skips secret rows. Their readers ask the settings store instead: the daemon's
  LLM config at boot, the Telegram token start, the reconciler, and `aura-media-index`, which
  decrypts with the `AURA_AUTHULA_SECRET` compose already gives `aura-ingest`.
- The skills installer runs `npx` with the same credential-free environment the stdio MCP
  children already get, never `os.Environ()`.

### Permissions

- PUT and DELETE of `AURA_OPENROUTER_MANAGEMENT_KEY`, `AURA_LLM_PROVIDER`, `AURA_LLM_MODEL`,
  `AURA_LLM_BASE_URL`, and the `llm-profile` route, require `identity.CapIdentityCreate`.
  Members keep `governance.write` for everything else.
- The first-run route step is shown to admins only.

### The resolver follows the live route

This lands before `IdentityLLM` is wired. `IdentityLLMResolver` reads the runtime snapshot's
config on every call and swaps in only the identity's API key. It decides whether the backend
bills per call from that live base URL. The reloader's apply clears its cache, so a model or
route change reaches cached identities on their next turn.

### The reconciler

`EnsureOpenRouterKeys` is a `Server` method in `internal/agui`.

- It does nothing on a local route, or when the management key is unset.
- It holds `settingsMu` and runs after a settings PUT has released it.
- It mints the services key when its row is empty. It writes the key with overrides built from
  the full settings list, so the route is never reset, through `ReplaceMany` plus
  `primaryLLMRouteReloader.Prepare`. If either fails after the mint, the new key is revoked.
- For every active non-service identity with no key, it mints one: no limit for an admin, a
  zero cap with a monthly reset for everyone else. The key is stored insert-if-absent
  (`ON CONFLICT DO NOTHING`); a mint that loses the race is revoked. A persist that fails also
  revokes (the existing T-02-31 pattern). The saga's `openrouter_key` step and
  `aura identity create` go through the same insert-if-absent path.
- One identity that fails does not stop the others. The joined error is returned, and the
  Credit panel shows that identity's no-key state.
- Triggers: saving the management key or switching to the OpenRouter route, daemon boot, the
  new-identity saga, and `aura identity create` when a management key is set.

### Admin status follows the capability

`handleGrantCapability` and `handleRevokeCapability` (`internal/agui/audit_api.go:178,182`)
PATCH the identity's key when `identity.create` changes: no limit when granted, a zero cap when
revoked. Deactivation PATCHes the key disabled, and reactivation enables it again.

### Turns

The serve path passes the resolver as `runner.Deps.IdentityLLM`. The chat environment is built
at `cmd/aura/chat_boot.go:560`, and the `aura chat` REPL keeps `IdentityLLM` nil
(`internal/runner/runner_deps.go:70-72`). `chat_boot.go` is already at 590 lines, so it is
split before this change. Web and Telegram turns are then billed to the identity's own key. A
missing key refuses the turn, with no fallback to the services key (CRED-07). The local route
stays exempt (D-13).

### "No limit"

- The next free migration (0125 today; the number is read from `ls internal/db/migrations` at
  landing) makes `aura.identity_llm_key.limit_usd` nullable, NULL meaning no limit. The down
  migration sets NULL rows to 0 and restores `NOT NULL DEFAULT 0`.
- `identitykey.Record.LimitUSD` and `Summary.LimitUSD` become `*float64`.
  `openrouterprovision.MintRequest.Limit` becomes `*USDCap`, and nil is sent as `"limit": null`.
  The PATCH gets a wire type that can send an explicit null, because `patchRequestWire`'s
  `omitempty` drops a nil.
- The credit API gains an explicit "clear cap" field; a nil `cap` keeps meaning "unchanged".
- `identitykey.Decide`: a nil limit allows; a limit ≤ 0 still refuses with no credit.
- Readers of the cap: the Credit panel shows "no limit" with no gauge. The over-allocation
  banner sums capped keys only and reports how many keys are uncapped.

### First-run wizard

- The model step becomes the route step. It is required while the route bills and no
  management key is set: `GET /api/onboarding/status` reports it, and the wizard cannot be
  dismissed while it is required. OpenRouter asks for the management key, the model and the
  services key's monthly cap; Ollama and llama.cpp ask for the base URL and the model.
- Saving runs the reconciler. The step shows the masked labels of the admin key and the
  services key, or the provider's error. After the first services key is minted it restarts
  Aura through the existing restart API (`internal/agui/restart_api.go`), and resumes once the
  daemon is back.
- `OPENROUTER_API_KEY` leaves `PRIMARY_SETTINGS`.
- The Credit panel renders a 409 as "this identity has no key yet", with the cause, instead of
  the generic load error.
- The refusals that told the operator to "set OPENROUTER_API_KEY in .env"
  (`cmd/aura/llm_client.go:16`, `internal/llm/config.go:19`, and `ErrNoIdentityLLMKey` at
  `internal/runner/runner_identity_llm.go:29`) point at the wizard.

### Installer, compose, `.env`

- `create-aura` drops the route, model and key prompts. `install.conf` moves to `format=2`
  without `llm_provider`, `llm_base_url`, `llm_model` and `openrouter_api_key`; the npm package
  carries the payload, so the two ship together. `install.sh` drops `apply_install_config` and
  the `OPENROUTER_API_KEY` line of its template. The template keeps the 13 variables compose
  requires (11 secrets plus the provenance values `AURA_EMBED_REVISION` and
  `AURA_EMBED_FINGERPRINT`) and the values that differ from compose defaults (`AURA_PROFILE`,
  `AURA_MUSR_ISOLATION`, `AURA_SANDBOX_IMAGE`, `COMPOSE_PROFILES`, `AURA_IMAGE`, the embedding
  model path and URL).
- `compose.yaml` stops passing `OPENROUTER_API_KEY`, `AURA_OPENROUTER_MANAGEMENT_KEY` and
  `AURA_LLM_PROVIDER/MODEL/BASE_URL` to `aura` and `aura-ingest`, and `TELEGRAM_BOT_TOKEN` to
  `aura`. The installer payload manifest is regenerated.
- `AURA_EMBED_DIMENSIONS` leaves `settings.AllowedKeys` and the Settings UI. The embedding model
  file fixes it, and changing it breaks the vector index. It stays an environment default read
  by the daemon and the Python ingest.
- `.env.example` keeps the required secrets and the documented infrastructure knobs. The 11
  names with no reader and the names equal to their compose default go.
- A test fails when `.env.example` gains a name that no code, compose file or script reads, or a
  value equal to its compose default.
- Updated with the change: `cmd/aura/container_artifacts_test.go`,
  `scripts/install_config_test.sh`, `packages/create-aura` `config-file` and `remote` tests,
  `scripts/musr_live_run_preconditions.sh`, `scripts/telegram_e2e.sh`,
  `scripts/ingest_reconcile_e2e.sh`, `web/src/settings/modelSettingsDefs.ts`.

## Delivery

Four plans, landed in this order so the tree never has a stack without a way to get a
credential:

- **A (daemon).** The resolver follows the live route; the migration and the null-capable wire
  types; encrypted secret settings and `OverlayEnv` skipping them; the reconciler with
  insert-if-absent, admin-only gating and the capability hooks; then `IdentityLLM` wired on the
  serve path.
- **B (UI).** The wizard route step and the Credit panel.
- **C (installer, compose, `.env`).** Never before A.
- **D (this PC).** The reinstall and the E2E.

## Testing

- **Unit.**
  - The resolver: follows a route and model change; the cache clears on apply.
  - The reconciler: idempotent; no-op on a local route and with no management key; no limit
    for an admin, the chosen cap for services, zero for everyone else; the route preserved when
    the services key is written; revoke when `Prepare` fails; one failed identity does not stop
    the rest; service identities skipped.
  - Insert-if-absent: a lost race revokes.
  - Capability hooks and deactivation send the right PATCH.
  - Permissions: a member gets 403 on the credential and route keys, and nobody can write
    `OPENROUTER_API_KEY` through the API.
  - Secret settings: encryption round trip, `OverlayEnv` skips secret rows, the boot converges
    plaintext rows, the skills installer env carries no credential.
  - `Decide` with a nil limit; mint and PATCH send `null`; clear cap; the over-allocation banner
    with uncapped keys.
  - The route step reported as required; the wizard cannot be dismissed while it is required.
  - The installer's `format=2`; the `.env.example` guard.
- **Integration (`db_integration`).** The migration up and down; `identitykey` Save and Load with
  a NULL limit under RLS; a prefixed secret row round trip through the settings store, and the boot step
  converging a plaintext row; insert-if-absent under concurrency.
- **E2E on this PC after the clean reinstall (Definition of Done).**
  1. A fresh install through the package, the first operator, then the wizard with route
     OpenRouter, the management key and a services cap. It restarts once.
  2. `GET /api/v1/keys` shows the admin's key (name = identity id, `external_user` set, `limit`
     null) and `aura-services` (the chosen monthly cap).
  3. An admin chat turn is billed to the admin's key (analytics `api_key_id`), and the spend page
     lists the admin.
  4. The Credit panel shows "no limit".
  5. A second identity is minted at zero, refused until topped up, and gets 403 on the route
     settings.

## Reinstall on this PC

1. Back up ArcadeDB to a file kept outside the volumes: the `aura-memory` MCP that Claude and
   Codex share lives there.
2. `docker compose down`, then remove every `aura_*` volume except the model and package caches
   (`aura-llama-embed`, `aura-llm`, `aura-ocr-vl`, `aura-npm-cache-host`, `aura-pip-cache-host`,
   `aura-uv-cache-host`); remove `~/.aura` and `.env`.
3. Install with the new package and run the E2E above.

## Not covered

- How a new `compose.yaml` reaches appliances that are already installed is the open question
  already recorded for de25355e7. Their plaintext secret rows are encrypted by the boot step
  above once they run the new image.
- Host CLI commands that read `OPENROUTER_API_KEY` from `.env` through godotenv lose it. They
  must read through the settings store, or run inside the container.
- Whether OpenRouter accepts `"limit": null` on `POST /keys` is documented (openapi.json types
  `limit` as number or null) but not yet measured. Plan A measures it first.
