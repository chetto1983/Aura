# Memory sidecar configuration from Postgres — design

Status: measured 2026-09-21, approved by the operator, implementation dispatched the same
day. This document is the brief the implementer was given, kept verbatim except for the
dispatch-only sections, because it is the record of what was measured and what was decided.

# Goal

`cmd/arcadedb-mcp` (the memory MCP sidecar) reads its whole configuration from
environment variables at process start. Postgres `aura.settings` is the configuration
authority for this product. Make the sidecar read its configuration from `aura.settings`
at boot, the same way the daemon already does, keeping in environment only the bootstrap
it cannot obtain any other way.

The operator's words: "env all'avvio non serve più, tutto su postgres."

# MEASURED FACTS — do not re-derive these, and do not contradict them without evidence

All verified today, 2026-09-21, on this tree.

1. Postgres is ALREADY the authority for the daemon; env is only the transport.
   `cmd/aura/config.go:158-189` builds a `settings.Store` over the pgx pool and calls
   `settings.OverlayEnv(ctx, store)`. That writes the allowlisted, non-secret
   `aura.settings` rows into the process environment with `os.Setenv`, and only then does
   `config.Load()` read the environment. DB values WIN over pre-set env.

2. `settings.OverlayEnv(ctx, l Lister)` (`internal/settings/settings.go:342`) is generic
   over a `Lister` interface; `*settings.Store` satisfies it. It SKIPS every row whose
   `AllowedKeys` meta has `Secret: true` — by design, so no credential ever reaches the
   process environment where child processes would inherit it.

3. Secrets are read separately through `Store.Secret(ctx, key)`. See
   `cmd/aura/boot_secret_settings.go` and `cmd/aura/chat_boot_settings.go:112-120` for the
   established shape.

4. THE PRECEDENT TO REUSE: `cmd/aura-media-index/main.go:245-249` is ALREADY a second
   binary that does exactly this — pool, `settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))`,
   `settings.OverlayEnv(ctx, store)`. Follow it. Do not invent a new mechanism, a new
   config package, an HTTP config endpoint, or a polling loop.

5. `cmd/arcadedb-mcp` currently imports NO pgx, NO `internal/db`, NO `internal/settings`.
   Its env reads are: `AURA_EMBED_BASE_URL`, `AURA_EMBED_MODEL`, `AURA_EMBED_API_KEY`
   (main.go:66-68, 74), `AURA_MEMORY_OPERATOR_DISPLAY_NAME` (114), `ARCADEDB_URL`,
   `ARCADEDB_DATABASE`, `ARCADEDB_USER`, `ARCADEDB_PASSWORD` (204-207),
   `ARCADEDB_TIMEOUT_SECONDS` (212), `AURA_ARCADEDB_MCP_PORT` (225),
   `AURA_ARCADEDB_MCP_HOST` (232), `ARCADEDB_ADMIN_USER`/`ARCADEDB_ADMIN_PASSWORD`
   (346-347), plus the `MCP_OAUTH_*` set in auth.go and the `AURA_MEMORY_*` bounds
   read through the helpers at main.go:283/295/307.

6. NETWORK: `arcadedb-mcp` and `postgres` are both on compose's implicit `default`
   network. Postgres IS reachable from the sidecar. Verified by reading compose.yaml.

7. `AURA_EMBED_API_KEY` IS DARK CODE. It is read at `cmd/arcadedb-mcp/main.go:68` and is
   set by NO compose file and NO `.env.example` — grep the whole tree, the only other hits
   are two live integration tests and a stale `.planning/` note. So the memory sidecar
   cannot authenticate to a cloud embedder today, at all. This is a real defect and
   CLAUDE.md says fix a gap found on touch. It must become a `Store.Secret` read, not a
   variable nothing sets. Check whether an equivalent secret row already exists in
   `AllowedKeys` before adding one; `AURA_MEMORY_EMBED_API_KEY` is currently asserted as
   NOT allowlisted by `internal/settings/settings_test.go:255,313` — if you change that,
   change the test deliberately and justify it in the commit message.

8. `AURA_EMBED_BASE_URL` and `AURA_EMBED_MODEL` are already in `AllowedKeys`
   (`internal/settings/settings.go:91-92`). compose maps `AURA_MEMORY_EMBED_BASE_URL` onto
   the sidecar container's `AURA_EMBED_BASE_URL` (compose.yaml ~line 793). Both containers
   resolve `aura-llama-embed:8081`. So the overlay makes the daemon and the sidecar AGREE
   on the embedder by construction — which is exactly what `.env.example` currently asks
   the operator to guarantee by hand ("It must AGREE with AURA_EMBED_BASE_URL above").
   Removing that manual burden is a goal of this change, not a side effect.

9. A commit landed 30 minutes ago, `2b7825cb4`, that fixed the daemon's embed route:
   `config.EmbedConfig` now has `BaseURL` (local only), `CloudModel` (the switch,
   `AURA_EMBED_MODEL`), `CloudBaseURL` (`AURA_EMBED_CLOUD_BASE_URL`, optional non-OpenRouter
   endpoint). Read `internal/config/config_embed.go` and `internal/config/config_routes.go`
   before deciding how the sidecar should resolve its embedder. Do not reintroduce the
   deleted loopback heuristic.

# THE SHAPE, AS APPROVED BY THE OPERATOR

- The sidecar's bootstrap env shrinks to what it cannot learn from Postgres: the Postgres
  DSN and `AURA_AUTHULA_SECRET` (needed to open sealed secret rows). Add them to the
  `arcadedb-mcp` service in compose.yaml, reusing the exact DSN composition the other
  services already use — read compose.yaml and do not invent a new variable name.
- Everything else the sidecar reads moves to `aura.settings` via `settings.OverlayEnv` at
  its boot, before any of its current env reads happen.
- The cloud embedding credential comes from `Store.Secret`, never from the environment.
- ArcadeDB's own credentials: decide and JUSTIFY. They are connection/security env, and
  `internal/settings`' package comment states explicitly that the allowlist exists so a
  settings row can never clobber connection/security env (`POSTGRES_*`,
  `ARCADEDB_PASSWORD`, `AURA_WEB_AUTH_SECRET`). Moving them would contradict that stated
  invariant. If you believe they should move anyway, argue it; if not, say so and leave
  them in compose. Either way the commit message must say which and why.
- Boot must FAIL LOUDLY if Postgres is configured but unreachable. A sidecar that
  silently falls back to stale env is the failure mode this whole change exists to
  remove. If you choose a fallback, it must be explicit and logged at WARN with the
  reason.

# YOU ARE ALLOWED TO REJECT THIS

If the evidence says the shape above is wrong, say so and propose what the measurement
supports instead — do not implement something you believe is wrong. A clean bill of
health is a valid outcome only if you went looking hard. The parts I am least sure of,
and most want attacked:

- Whether `OverlayEnv`'s `os.Setenv`-then-read-env indirection is right for a process that
  could just read the rows directly into its config struct. It is the established pattern,
  which is an argument, but not proof it is the right one here.
- Whether boot-time-only is acceptable. `.env.example` currently says the sidecar's
  embedder is "NOT runtime-overridable via the cockpit Settings page (the daemon's
  os.Setenv overlay cannot reach an already-running sidecar)". Reading from Postgres at
  the SIDECAR's own boot does not by itself make it live — it makes it correct at restart.
  If you think a reload path is required for this to be worth doing, say so; do not build
  one unasked.
- Whether adding a Postgres dependency to a sidecar whose health gate the `aura` service
  depends on introduces a boot-order hazard. `compose.yaml` shows `aura` gates its boot on
  this sidecar's healthcheck, and the healthcheck is deliberately liveness-only. Check
  `depends_on` for postgres and reason about the startup graph.

# Constraints carried into the plan

See `docs/superpowers/plans/2026-09-21-memory-sidecar-settings-from-postgres.md`. The
project-wide rules (600-LOC cap, migration numbering from the directory, no skip-as-green,
real tests) are in CLAUDE.md and bind every task.
