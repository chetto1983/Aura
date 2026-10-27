# Unwired features and duplicates, 2026-10-26

Asked by the operator on 2026-10-26: "fai la scansione delle funzionalità non cablate e duplicati"
(scan for features that are not wired and for duplicates).
- **Tree.** `63b8919`, right after the dead-code cleanup `a96db3d..63b8919` (22 commits, one per
  package). That cleanup deleted everything `deadcode -test` could prove unreachable, so what is
  left here is the kind of debt that tool cannot see.
- **Cleanup check.** The `db_integration` coverage gate was run on that tree through
  `scripts/coverage_docker.sh`, against the disposable `aura_cov` database.
  - Owned surface: 49,811 of 56,985 statements, 87.41%, against the 85% floor.
  - Package policy: 100 packages evaluated, 92 at target, 6 on their pinned baselines, 2 delegated.
    It passed.

  This is one local run, not CI: branch pushes trigger no workflow.
- **Where this file lives.** Not in `docs/audit/`: `scripts/audit_closure_gate.py` pins that
  directory to `README.md` alone (`EXPECTED_AUDIT_PATHS`), and a second file there fails the gate.
  Nothing below is a register row; promoting one is the operator's call, because a row turns
  `release_ready` off.

## Verdict

One bug, a set of half-finished consolidations, and a set of features built up to the last wire.

- **Bug, U0.** `POST /api/conversations/{id}/export-delete` deletes the conversation, then answers
  with a download URL that the parent mux does not route. This was measured: 404, and AG-UI is
  never reached. No cockpit code calls it.
- **Duplicates with a shared home already written, D1 and D2.** `secret.Sealer` and
  `db.IsUniqueViolation` each say in their own comment which copies they replace, and those copies
  are all still in the tree:
  - four crypto constructions and one derivation;
  - five unique-violation checks.

  This is the most expensive kind of duplicate, because the fix already exists and the drift
  continues anyway.
- **Unwired, U1-U6.** Each is fully built and tested code, missing one connection:
  - a pause TTL that is switched off;
  - an MCP Apps capability that is never declared;
  - an MCP audit ledger with no read path;
  - an OAuth leeway that claims 60 s when the code applies 10 s;
  - a metric that is declared and never recorded;
  - 11 groups of HTTP routes with no production caller (20 routes), one of them a stub.

  None is a bug. Each is a decision, wire or delete, that the operator owns.
- **Noise.** 13 dead parameters and results, all in signatures the code outgrew.

`dupl` at the CI threshold (100) finds nothing. Everything here is below that gate, or is the kind
of duplicate it cannot see: same behaviour in a different shape.

## Closure, 2026-10-26

Asked the same day: "Parti a chiudere tutto e validalo" (close everything and validate it). The
closure is `e4f87d1..HEAD` on branch `ccr-648b75d9-4hbqnw`, one commit per decision. Every
finding below was either wired, deleted or folded, or is listed under "Left to the operator"
with the reason it was not.

### Bugs fixed

| Finding | Commit | What changed |
|---|---|---|
| U0, owner-export download 404 | `e4f87d1` | `GET /api/owner-exports/{id}` is mounted. `TestServeWebuiMountsEveryAGUIRoute` reads every `mux.Handle` pattern in `internal/agui` from source and drives each through `newServeHandler`, so a third unmounted route fails a test instead of a user. |
| U4, 10 s OAuth leeway | `87b9865` | `refreshingTokenSource` wraps every refreshing source in `oauth2.ReuseTokenSourceWithExpiry(…, oauthTokenLeeway)`: reload, restore and first authorization. |
| U5, span-export metric never recorded | `a996f56` | The exporter is wrapped by `countSpanExportFailures`, which increments the counter on every failed `ExportSpans`. |
| U1, pause TTL switched off | `ac09355` | The runner's pause store is `askuser.NewWithPauseTTL(pool, cfg.AskUser.PauseTTLSec)`. |
| New: asset retry re-armed any status | `b6dbbc3` | `POST /api/assets/{id}/retry` put a complete or processing asset back to `accepted`, so a retry of a success re-ingested it and a retry of a running job raced the worker. `Store.ResetForRetry` uses the guarded `ResetAssetForIngestionRetry`, which had no caller; anything else is `ErrNotRetryable`. A `db_integration` test proves the SQL guard. |
| New: empty-key session fallback | `65429f7` | With no `SessionValidator`, `RequireAuth` fell back to an HMAC cookie under `AuthDeps.SigningKey`, which production set to nil; a cookie MAC'd under an empty key would have passed. It now fails closed, and the passphrase cookie code, its three `AuthDeps` fields and `POST /logout` are gone. |
| New: rune-splitting truncation | `59ae93f`, `55e1a2c` | `agenteval` and the MUSR live assert cut strings by byte. `mcptools.truncateUTF8Bytes` was O(n²) on invalid UTF-8. All four truncations now go through `internal/utf8cut`. |

### Removed: built, not connected, and not worth wiring

| What | Commit | Why delete, not wire |
|---|---|---|
| U2, `mcp.AppsClientSettings` | `5e04232` | One connection per server is shared by the cockpit, Telegram and the CLI; declaring the capability would let servers trim text the other two need. Views render without it. |
| U6, worker transcript pull route | `be9b12e` | Superseded by the push-only SSE worker streams. |
| U6, `remote-access/rotate-token` stub | `c43ed9c` | It always answered 409; the cockpit shows the same guidance itself. |
| U6, `POST /api/documents/labels/{id}/run` | `794871a` | Saving a label already runs its rule. |
| U6, `POST /logout` | `65429f7` | It cleared only the passphrase cookie; sign-out is Authula's. |
| `AURA_WEB_AUTH_PROVIDER`, `AURA_WEB_AUTH_SECRET` | `9c7a25c` | Loaded and never read once the boundary became Authula-only. |
| Telegram local Bot API trio | `7e98b3d` | `TELEGRAM_API_BASE_URL`, `TELEGRAM_FILE_BASE_URL` and `AURA_TELEGRAM_LOCAL_BOT_API` reached no `tele.Settings`. Wiring the URL alone would not work: in `--local` mode `getFile` returns a path on the server's disk (tdlib `telegram-bot-api` README), so a local server needs a service, a shared volume and a reader. `docs/asset-pipeline.md` now states the real 20 MB limit. |
| `AURA_CONFIG_DIR` | `a9a3ef9` | Set by compose and `.env.example`, read by nothing. |
| `ArcadeDBConfig.User/Password`, `KnobSpec.Default/Secret` | `5128f6e` | The pair belongs to the `arcadedb-mcp` sidecar, which reads it itself. The knob fields fed doc generation and redaction that do not exist. |
| 17 sqlc queries | `d32a4d1` | Grepped across `internal/` and `cmd/` with tests; `sqlc generate` (v1.31.1) leaves no diff. The tables behind them stay: see below. |
| Dead parameters and results, `garageadmin.Option` | `3d1b61f`, `1f05262` | The 13 `unparam` hits, plus the nil parent span threaded through about twenty methods. |

### Duplicates folded

| Finding | Commit | Shared home |
|---|---|---|
| D1 | `aafac9e`, `fd3e69e` | `secret.DeriveKey` and `secret.NewSealerFromKey`. Six `hkdf.Key` call sites became one; every HKDF info label is unchanged, and a vector test proves rows written by the old copies still open. |
| D2 | `4d9a270` | `db.IsUniqueViolation`, five private copies deleted. |
| D3 | `96e17f6` | `internal/activesources`, imported by both sides of the recall-context wire. |
| D4 | `98aa9ce` | `sidecarBaseURL`, one WR-01 port guard. |
| D5 | `1eee0aa` | `mcp.DrainTools` beside `BoundedCall`. |
| D6 | `bb9f4f0` | `idempotency.Registry` and `idempotency.ReplayRetention`. |
| D7 | `074a997`, `4043128`, `08fd19e`, `55b8585`, `d8a4d8e`, `55e1a2c`, `d4212e5`, `b0d532f` | `internal/asciifold`, `pgnumeric.FloatFromAggregate`, `envutil.BoolDefault`, `conversations.Excerpt`, `pgtype.UUID.String` with `db.ParseUUID` and `db.OptionalText`, `internal/utf8cut`, `isNilDependency`, `SessionIDFromContext`. |

### Corrections to this report and to the docs

- **U3 was half wrong.** The cockpit does read `aura.mcp_audit`: `GET /api/admin/audit` unions it
  with the other identity ledgers (`internal/agui/audit_store.go`). What was dead is the second
  reader, `MCPAuditStore.List`, whose constructor already lived behind `db_integration`. It moved
  under that tag (`8a44f79`); production keeps the write half.
- **The `/setup` "wizard"** is a token-gated JSON API. README, `docs/INSTALL.md` and the installer
  now point at the cockpit (`8966729`).
- **The update policy.** `prd.md` and `packages/create-aura/README.md` still described a 15-minute
  idle trigger and `AURA_UPDATE_IDLE_SECONDS`; the updater has applied at night from
  `AURA_UPDATE_AT` since `7b1a23e` (`ab6ffd1`).
- **The env catalogue.** 23 knobs read by the process appeared in no document. They are now in a
  `.env.example` section that says compose forwards none of them (`3984c74`). CLAUDE.md pointed to
  a PRD env index that no longer exists (`62f6e2a`). `AURA_RECOVERY_*` stay out: they are one-shot
  inputs to `aura identity recover-operator`, documented by its usage text.
- **Found by the closure's own gate.** `b6dbbc3` took `internal/assets/service_test.go` to 634
  lines; `make quality`'s file-size check caught it before a push, and the fake store moved to
  `fake_store_test.go`.

### Kept on purpose

- `Event.ParentSpanID` and `InvocationContext.ParentSpanID`: the documented forward-compatible
  wire shape for span chaining (WR-04). Only the plumbing went.
- `RunSession.append`'s `bool`: production ignores it by design and the backpressure tests pin it.
- `retention.ratioFromCapacity` and `mcp.countTools`: the first is called from a Windows build
  file; the second counts under the probe's own per-transport boundary and collects nothing, so
  `DrainTools` would replace its bound and allocate a slice to read its length.
- Duplicates of shape only: `openRouter` List/ListKeys, `newPasswordResetToken`/`newRecoveryCode`,
  `stringSet`/`finiteSet`, `buildArcadeMemoryPurger`/`buildArcadeMemoryProvisioner`.
- `GetIdentityRecoveryByIdentity`, `ListIdentityAudit` and `GetRun`: queries only tests call, as
  the typed read-back of what production writes. Replacing them with hand-written SQL in tests
  would lose sqlc's check against the schema.

### Left to the operator

Each needs a decision this closure should not take alone.

- **Dropping tables or columns.** Decided by the operator on 2026-10-27 and done by migration
  0143, recorded in `prd.md`:
  - `aura.benchmark_settings_overrides` and `aura.benchmark_settings_override_rows`, whose
    queries went in `d32a4d1`;
  - `aura.asset_events`, written only by RLS tests;
  - `aura.audit_logs`, which nothing writes or reads, and whose `ON DELETE RESTRICT` foreign key
    `cmd/aura/serve_provisioning.go` warned would break de-provisioning once something did;
  - `pipeline_generation` on `aura.assets`, `aura.ingestion_jobs` and `aura.ingestion_events`.
    No producer sets it. This list first called it always 0, which was too strong: 0093's
    backfill could have copied a generation from `aura.documents`, which 0098 dropped, so the
    column goes whatever it holds.

  The migration refuses to run while any of the four tables holds a row.
- **Write-only audit tables.** `aura.ingestion_events`, `aura.identity_recovery_audit` and
  `aura.identity_audit` (the admin feed does not union the last one) are written and never read.
  Whether an audit table needs a reader is a product call.
- **Gates that check a self-reported number.**
  - `gateReplication` refuses `server_production` unless `AURA_OBJECTSTORE_REPLICATION_FACTOR >= 2`,
    but nothing compares the knob with Garage, whose config pins `replication_factor = 1`.
    Setting the knob to 2 passes the gate and changes no durability. The honest gate reads the
    factor from Garage's admin API.
  - `AURA_EMBED_REVISION` and `AURA_EMBED_FINGERPRINT` are required by compose and derived by the
    installer, yet only checked for presence and hex. `embeddings.AttestLocal` already names the
    served model from the sidecar, and its comment says the fingerprint cannot. Either compare
    them, or drop the pair and the installer step.
- **U6 routes with no production caller, kept:**
  - `GET /api/conversations/{id}/branches` and `POST …/branches/{branchSeq}/select`. The cockpit's
    branch navigation is client-side only (`ExternalStoreChat_branches.ts` posts only to `/edit`).
    Not verified live: after navigating to an older sibling, the next message may continue the
    server's newest branch, and siblings may not survive a reload. A cockpit test decides whether
    this is a bug to wire or routes to delete.
  - `GET /api/fetch`: its comment says cockpit-only and only `web/e2e` calls it.
  - `GET /api/settings/remote-access/events`, the admin capability grant routes,
    `POST /api/admin/openrouter/reconcile`, `/debug/vars` and `/metrics` on `:9080`, and the
    cloudflared supervisor's `/status`: manual or operational surfaces, used by hand in
    verification files.

### Validation

Measured locally in the cloud session on 2026-10-26 and 27, not on the lab VM and not in CI.
CI runs only on pushes and pull requests to `master`, so it had not seen this branch yet.

| Gate | Result |
|---|---|
| `make quality` (deadcode, vet, file-size, capability declaration, model contracts, lint, race, vuln, build) | Green. The first run stopped at file-size (`service_test.go`, see above). The second passed every step up to `vuln`, whose binary was missing from the container; `vuln` and the final build then passed on their own. `golangci-lint` reported 0 issues. `go test -race` passed on 108 packages. `govulncheck` v1.6.0 found 0 vulnerabilities that the code calls. |
| `scripts/coverage_docker.sh`, `db_integration`, on the disposable `aura_cov` | 49,616 of 56,733 owned statements, 87.46%, against the 85% floor. The package policy failed closed on four baselines the folds had moved; `b1c8f00` re-pins them, then 103 packages are evaluated and 95 are at target. |
| Mutation, the five scopes whose packages changed | gateway 78.95%, elicitation_route 80.00%, profile_validation 100%, ssrf 100%, turn_reading 100%, each against the 70% floor. identity_isolation, sandbox, pausable and the two media scopes were not rerun: their packages did not change. |
| `build-and-lint`'s other gates, run one by one | 34 passed. `tempo_blocks_repair_test.sh` refuses to run as root, as the cloud container is. It tests nothing this branch touches. |
| `sqlc generate` (v1.31.1) | No drift. |
| `scripts/payload_manifest_gate.sh` | Failed until `843bfda`: three commits had changed `compose.yaml` without re-hashing it. |

**What this does not prove.**
- No live E2E. The owner-export download, the asset retry guard and the session fail-closed were
  each proven by a handler or `db_integration` test, not by an agent on the running stack.
- The web, Stryker, E2E and integration-tier jobs that need their own services ran only in CI.
- The crypto fold was proven on vectors written by the old copies. The live stack in this
  session held no encrypted rows.

## Method

Every finding below was read in the source after a tool flagged it. A tool hit that did not
survive reading is listed under "Looked at, not findings".

| Pass | Command or tool | Scope |
|---|---|---|
| Reachability, production only | `deadcode ./...` (no `-test`) | 40 entries; 31 are test-support packages by design |
| Unused results and parameters | `golangci-lint run --default=none --enable=unparam --tests=false` | production code |
| Symbols only tests reach | the same run's `unused` with `--tests=false` | production code |
| Identical function bodies | AST body hash over every non-test function (scratch tool) | whole module |
| Same name, several definitions | `grep '^func <name>'`, then bodies compared by hand | whole module |
| Near clones | `dupl` at threshold 60 (the CI gate runs at 100 and is green) | non-test Go |
| Declared metrics never recorded | every `InstrumentID` in `internal/obs/catalog.go` against its recording sites | `internal`, `cmd` |
| HTTP routes, Go against cockpit | Every `HandleFunc`/`Handle` pattern was listed. A Go 1.22 pattern matcher checked them against the `newServeHandler` mounts, and every `web/src` call site was matched against them. | 265 routes, 227 call sites |
| sqlc queries, tables, env knobs | Every query name, table and `os.Getenv`/`envutil` key grepped against its readers and writers (agent pass, then read by hand) | Its findings are under Closure |

## Unwired: built, documented, not connected

Each item below has a decision attached, wire or delete, and that decision is the operator's. The
code exists, carries tests, and in most cases a comment describing behaviour the running system
does not have. U0 is the exception: it is a bug in a route that is reachable today.

### U0. `export-delete` deletes the conversation and returns a download link that 404s

The owner-export trio is registered on the AG-UI mux (`internal/agui/conversations_api.go:74-76`):
- `GET /api/conversations/{id}/owner-export`;
- `POST /api/conversations/{id}/export-delete`;
- `GET /api/owner-exports/{id}`.

The parent mux (`cmd/aura/serve_webui.go`, `newServeHandler`) mounts the
`/api/conversations/` subtree, so the first two are reachable. Nothing mounts
`/api/owner-exports/`, and `/api/` is an SPA-fallback exclusion, so the third gets a backend 404.

**Measured.** A throwaway test drove `newServeHandler` with a valid session, then was deleted:

| Request | Response | Reached AG-UI |
|---|---|---|
| `GET /api/conversations/c1/owner-export` | 200 | yes |
| `GET /api/owner-exports/e1?expires=1` | **404** | **no** |

`handleOwnerExportMode` with `deleteAfter=true` (`internal/agui/retention_api.go:116-169`) runs
`OwnerExporter.ExportDelete`. That writes the export to the durable store, **deletes the
conversation** (`Deleter: s.run`), and answers 201 with
`download_url: "/api/owner-exports/<id>?expires=…"` (`:163`). That URL is the 404 above. So a
caller of `export-delete` loses the conversation and cannot fetch the export that was meant to
replace it; the export sits in the store until it expires.

No cockpit code calls any of the three routes: `grep owner-export|export-delete|owner-exports`
over `web/src` returns nothing. The bug is therefore latent, but one `curl` away. The routes are
behind the session gate, not behind a capability.
- **If the trio stays:** mount `GET /api/owner-exports/{id}` in `newServeHandler`, plus a test on
  the serve handler like the probe above.
- **If it goes:** delete all three, with their handlers, `OwnerExporter` and the durable
  destination. Do not keep `export-delete` alone.

### U1. The lazy pause TTL on `ask_user` is built and switched off

`internal/askuser/store_fencing.go:94-102` `NewWithPauseTTL` makes `GetByToken` treat a pending
pause older than the TTL as `ErrPauseNotFound`, "before the sweeper ever claims it". Its own
comment says every production call site "still uses [`New(pool)`] today, so this constructor is
additive and inert until a caller opts in". Both call sites still use `New(pool)`:
`cmd/aura/chat_boot.go:357` and `cmd/aura/paused_states.go:56`.

The knob it would read, `AURA_ASKUSER_PAUSE_TTL_SEC` (default 172800), is wired today only to the
sweepers (`cmd/aura/serve.go:507`, `cmd/aura/serve_delegation.go:421`). So between the TTL
elapsing and the next sweep, an expired pause can still be answered and resumed.
- **Wire:** `askuser.NewWithPauseTTL(pool, cfg.AskUser.PauseTTLSec)` at `chat_boot.go:357`. That
  is one line, and the `<=0` contract already matches the knob's.
- **Delete:** `NewWithPauseTTL`, the `pauseTTLSec` field and both `pauseExpired` helpers.

### U2. The MCP Apps extension is never declared, though the cockpit renders views

`internal/mcp/apps.go:94-104` `AppsClientSettings` is the capability object that tells a server
Aura renders `ui://` views. Its comment sets the condition: declare it "only once a surface
actually renders views". The cockpit does render them (`web/src/chat/mcpapps/McpViewFrame.tsx`),
and the bridge does attach the view descriptor to results (`bridge_views.go`, `viewMetaKey`).
But `internal/mcp/sdkclient.go:110` sends an empty `&sdkmcp.ClientCapabilities{}`, and the
installed go-sdk has the `Extensions` field this would go in. So `AppsClientSettings` is reached
only by `apps_test.go:186`.

This is a real design choice, not an oversight to patch. A capability is declared per
connection, not per surface. Its comment warns that a server which sees it may trim its text,
which Telegram and the CLI need. Options:
- wire it for cockpit-only connections;
- decide the trade is worth it everywhere;
- delete the function and keep the views that servers send unasked.

### U3. The MCP audit ledger is written and cannot be read

Every MCP config mutation writes a row inside its own transaction (`InsertMCPAuditTx`).
`internal/mcp/manager/audit.go:97` `MCPAuditStore.List`, the sqlc query `ListMcpAudit` and
`mcpAuditFromRow` have no production caller. The parallel skills ledger is readable:
`GET /api/governance/skills/audit` (`internal/agui/governance_api.go:122`, `:345`) over
`skills.AuditStore.List`. There is no MCP equivalent route, so the operator cannot see the
history the system keeps.
- **Wire:** `GET /api/governance/mcp/audit` on the skills route's pattern, plus its cockpit view.
- **Delete:** `List`, `mcpAuditFromRow`, the query, and the claim in the docs that MCP changes
  are auditable from the cockpit, if one exists.

### U4. The OAuth refresh leeway says 60 s; the code applies oauth2's 10 s

`internal/mcp/oauth_tokensource.go:45-47`:

> `oauthTokenLeeway` refreshes a token slightly before it dies so a call in flight is not the
> thing that discovers the expiry.

Its value is `60 * time.Second`. Its only reader is a test (`oauth_tokensource_test.go:415`).
The refresh path builds its source with `oauth2.Config.TokenSource` (`oauth_tokensource.go:262`,
`:340`; `oauth_handler.go:291`). That wraps a `reuseTokenSource` with oauth2's
`defaultExpiryDelta`, which is **10 s** (`golang.org/x/oauth2@v0.37.0/token.go:22`).

The constant describes behaviour the system does not have. `Token.ExpiryDelta` and
`oauth2.ReuseTokenSourceWithExpiry` (`oauth2.go:402`) both exist to set it.
- **Wire:** set `ExpiryDelta: oauthTokenLeeway` where `tokenFrom` builds the token.
- **Delete:** delete the constant and correct the comment to say 10 s.

### U5. `aura_agent_span_export_failures_total` is declared and never incremented

`internal/obs/catalog.go:69` and `:128` declare it as "Total OpenTelemetry span export failures."
No code records it. The natural site exists: `internal/obs/otel_error_handler.go`. It receives
every export failure the SDK reports, but only logs them, rate-limited, with a suppressed count in
the log line. A dashboard or alert built on this series reads a permanent zero, which is a false
green.
- **Wire:** increment it in `rateLimitedOTelHandler.Handle`. The handler sees every error,
  including the suppressed ones, so the counter would be exact.
- **Delete:** drop the descriptor.

The other 40 of the catalog's 41 `InstrumentID`s each have a recording site (`BoundaryInFlightID` records inside `internal/obs/boundary.go`).

### U6. HTTP routes that nothing calls

All 265 backend routes were compared with all 227 cockpit call sites and with every other client:
- the backend routes are the 235 on the AG-UI mux, the 16 that exist only in `cmd/aura`, and the
  setup, cloudflared and arcadedb-mcp listeners;
- the cockpit call sites are fetch calls, JSON wrappers, 3 `EventSource` streams, img and iframe
  sources, and the SVAR REST providers;
- the other clients are `scripts/`, compose healthchecks, Caddy, Prometheus,
  `packages/create-aura`, the channels and the docs.

**Every cockpit call reaches a route.** The opposite direction is not clean. "No caller" below
means no production client. A route reached only from `web/e2e`, or by hand in a verification
file, counts as unused.

| Route | Where | Callers |
|---|---|---|
| owner-export trio | `internal/agui/conversations_api.go:74-76` | none. See U0. |
| `GET /api/conversations/{id}/branches`, `POST …/branches/{branchSeq}/select` | `internal/agui/conversations_branch_api.go:40`, `:42` | None. The cockpit builds branches locally (`web/src/chat/ExternalStoreChat.tsx:445`) and calls only `/edit`. The select route is still mounted with a capability gate (`cmd/aura/serve_webui.go:114`). |
| `GET /api/conversations/{conv}/swarm/{childID}/transcript` | `internal/agui/server_swarm_transcript.go:41` | none; worker status is push-only over SSE (`server_swarm_events.go`) |
| `GET /api/fetch` (data proxy) | `internal/agui/server.go:413`, `data_proxy.go:35` | Its comment says "cockpit-only". The only caller is `web/e2e/origin-isolation-live.spec.ts`. |
| `GET /api/settings/remote-access/events` | `internal/agui/remote_access_api.go:74` | `remoteAccessApi.ts` calls every other remote-access path, but not this one |
| `POST /api/settings/remote-access/rotate-token` | `remote_access_api.go:76`, `:153-156` | None, and it is a stub: it always answers 409 `manual_rotation_required` with a Cloudflare docs URL. The UI shows that link itself (`RemoteAccessStatus.tsx:177-184`). |
| `POST /api/documents/labels/{id}/run` | `internal/agui/documents_labels_api.go:18`, `:64` | none; saving a label already runs its rule |
| `POST`/`DELETE /api/admin/identities/{id}/capabilities[/{capability}]` | `internal/agui/audit_api.go:84-85` | No admin UI. The only use is manual `curl` in `docs/superpowers/verification/2026-10-10-member-deployment-administration.md`. |
| `POST /api/admin/openrouter/reconcile` | `internal/agui/openrouter_reconcile.go:60` | the only use is a browser-console step in a 2026-09-10 plan |
| `POST /logout` | `cmd/aura/serve_webui.go:96`, `internal/agui/auth.go:142` | None. The cockpit signs out through Authula's `/auth/sign-out`, and `AppShell.shell.test.tsx:412` checks that `/logout` is never called. |
| `/setup/*` on `:9081` | `internal/setup/server.go:137-142` | Only `scripts/telegram_e2e.sh` calls it, and nothing calls `/setup/events`. |

**Documentation drift.** `README.md:129` and `docs/INSTALL.md:137` send the operator to
`https://<host>/setup/?token=…` as the "setup wizard". That URL is a 307 to the JSON
`/setup/status` (`internal/setup/server.go:146-153`), and there is no page behind it. Telegram is
connected from cockpit Settings through its own routes. Either the docs or the setup server is
wrong.

**Not classified.** These may be kept on purpose for manual debugging; nothing proves either way:
- `GET /debug/vars` and `GET /metrics` on the main `:9080` listener. Prometheus scrapes the private
  `:9464` listener (`observability/prometheus/prometheus.yml`, `cmd/aura/serve_observability.go:61`).
- the cloudflared supervisor's `GET /status`.

## Duplicates

The two worst cases share one shape: someone extracted a shared helper, wrote down in its comment
which copies it replaces, and never deleted those copies. The helper adds a copy instead of
removing them.

### D1. `secret.Sealer` exists to replace four crypto copies; all four are still there

`internal/secret/sealer.go:15-28` says it exists "because that exact construction had been
written three times", and names `internal/mcpoauth`, `internal/objectstore` and
`internal/agui/recovery_hash`. A fourth was about to be written for the MCP registry.
`mcpregistry`, `identitykey`, `pimprovider` and `chatgptplan` use it. Copies of the same
HKDF-SHA256 → AES-256-GCM, nonce-prepended construction that remain:

| Copy | What it duplicates | Fold |
|---|---|---|
| `internal/mcpoauth/store.go:301-388` | `seal`, `sealOptional`, `open`, `openOptional`, `deriveKey`, `deriveKeyWithInfo`: the whole `Sealer`, line for line | `secret.NewSealer(secretHex, keyDerivationInfo)`. The wire format is identical, so stored grants keep opening. |
| `internal/objectstore/identity_store.go:177-231` | `encrypt`, `decrypt`, `deriveObjectStoreKey` | same; same wire format |
| `internal/tracesink/sink.go:91-113` | `encrypt` and `deriveKey`, over `AURA_TRACE_ENCRYPT_KEY` | Same wire format. `secret.deriveKey`'s error text names `AURA_AUTHULA_SECRET`, so the message needs a parameter before this one folds. |
| `internal/settings/secrets.go:43-55` | `aeadWithInfo`: the derivation and the AEAD | Derivation only. Its wire format is different (`enc:v1:<nonce hex>:<ct hex>`), and stored rows depend on it. |
| `internal/agui/recovery_hash.go:81-95` | the HKDF derivation half, for a pepper | needs `secret.deriveKey` exported |

Domain separation holds today. All ten HKDF info labels in the tree are distinct, from
`aura-mcp-oauth-identity-key-v1` to `aura-browser-state-v1`. The fold has to carry each label over
unchanged, or stored ciphertext stops opening. There are 6 `hkdf.Key` call sites, where there
should be 1. A fix to the derivation (input
validation, a key-length change) now has five places to land, and the type written to prevent
exactly this is the sixth.

### D2. `db.IsUniqueViolation` exists; five private copies remain

`internal/db/unique_violation.go:9-17` says it lives there "because more than one package needs
it and each had grown its own copy (internal/channels/telegram, internal/cron)". Its production
callers: 2. Byte-identical private `isUniqueViolation` copies: `internal/identity/store.go:268`,
`internal/runner/runner_conversation.go:97`, `internal/cron/store.go:438`,
`internal/channels/telegram/store.go:254` and `cmd/aura/serve_onboarding.go:319`.
Every one of these packages already imports `internal/db`, so the fold creates no import cycle
(checked with `go list`).

### D3. The recall-context wire codec is written twice, with no drift test

Client side: `internal/agent/mcptools/bridge_recall_context.go:16-23`. Server side:
`cmd/arcadedb-mcp/tool_memory_recall.go:25-33`. Both hold the header name, version, max sources,
max ID runes and the decoded and encoded size caps, plus mirror-image envelope structs. The server
comment says they are "defined independently … because this binary and the agent bridge share a
loopback wire contract, not a Go package boundary". Both live in one Go module, and
`cmd/arcadedb-mcp` already imports `internal/arcadedb`, so the boundary is not a reason. The
comment's own requirement, "must stay byte-identical on both sides", is checked by no test (no
`_test.go` file contains the header literal). Fold: one `internal/` package holding the constants
and the encode/decode pair, imported by both sides.

### D4. Sidecar URL with the WR-01 port guard, four copies

`internal/mcp/manager/catalog.go:20-74`: `memoryRecipeURL`, `whatsappRecipeURL`,
`PIMSidecarBaseURL` and `WhatsAppBridgeBaseURL`. Each does the same three things:
- Compose DNS when `AURA_IN_CONTAINER=1`;
- otherwise read a port env;
- validate it as 1-65535, or fall back.

The validation is a security guard: the WR-01 userinfo retarget. One copy that drifts reopens it.
Fold: one `sidecarURL(containerURL, portEnv, defaultPort, path)`.

### D5. Draining an MCP tool list, three copies

`internal/agent/mcptools/bridge_supervisor.go:248` `drainTools` and `cmd/aura/mcp_tools.go:168`
`drainSDKTools` are identical: `mcp.BoundedCall` around `session.Tools`. The second one's comment
says it "mirrors" the first. `internal/mcp/probe.go:97` `countTools` runs the same loop and counts
instead of collecting. Fold: one `mcp.ListTools(ctx, session)` next to `BoundedCall`, which is
where the reason for the bound is documented.

### D6. The `operationRegistry` interface is declared three times

`cmd/aura/idempotency.go:47`, `internal/agui/idempotency_http.go:25` and
`internal/gateway/gateway.go:36` each declare the same three methods: `Begin`, `Complete`,
`MarkIndeterminate`. The idempotency retention is likewise three separate
`30 * 24 * time.Hour` constants:
- `cmd/aura/idempotency.go:43`;
- `internal/agui/idempotency_http.go:23`;
- `internal/gateway/reserve.go:48`.

Consumer-side interfaces are idiomatic Go, so this is the mildest finding here. The retention is
not idiomatic: it is one policy written three times.

### D7. Small helpers with identical bodies

| Helper | Copies | Fold target |
|---|---|---|
| `asciiCaption` | `internal/agent/tools/send_file.go:255`, `internal/channels/telegram/artifact.go:204` | one of the two packages, exported |
| `anyCostFloat` / `anyNumericFloat` | `internal/agui/credit_ledger.go:118`, `internal/cachemetrics/store_helpers.go:102` | `internal/pgnumeric`, which both already call |
| `envBool` / `envutil.BoolDefault` | `internal/llm/config.go:518`, `internal/envutil/envutil.go:37` | `envutil`. `llm` does not import it yet, and `envutil` is a leaf package. |
| `clampExcerpt` | `cmd/aura/chat_repl.go:330`, `internal/channels/telegram/commands.go:493` | |
| `uuidString` | 7 definitions; `internal/assets/store.go:418` = `internal/documents/pgvalues.go:27` | |
| `pgUUID` | `internal/assets/store.go:397` = `internal/documents/pgvalues.go:19` | |
| `optionalText` | 4 definitions; `internal/approvalgrants/store.go:169` = `internal/approvalpolicies/store.go:192` | |
| `truncate` | `internal/agenteval/case.go:166` = `scripts/musr_live_run_assert.go:224` | |

UTF-8-safe byte truncation is written four ways:
- `internal/agent/tools/result.go:247` `truncatePreview` backs off with `utf8.RuneStart`.
- `internal/agent/llm_agent_finalize.go:217` `truncateBytes` does the same, without the
  `capBytes <= 0` guard.
- `internal/agent/tools/read_file.go:298` `truncateRuneBoundary` does the same with a hand-written
  `0xC0` mask.
- `internal/agent/mcptools/bridge.go:356` `truncateUTF8Bytes` is the one that behaves differently.

`truncateUTF8Bytes` loops `utf8.ValidString` and drops one byte per iteration. On text with an
invalid byte anywhere before the cut, it therefore trims back past that byte. That costs
O(maxBytes²) and returns less text than the other three would. Its input is MCP server text and
error strings, which Aura does not control. One helper in a leaf package removes the question of
which one is right.

## Dead parameters and results

`unparam` over production code, each hit read. None changes behaviour; each is a signature that
outlived the code that used it. They are listed so the next edit of each file removes them
(DEEP REFACTOR ON TOUCH), not as a reason to open those files now.

| Site | What is dead | Note |
|---|---|---|
| `internal/agui/runsession.go:123` `(*RunSession).append` | the `bool` result: both callers (`server_run_detach.go:167`, `:202`) discard it | The doc comment still says the result exists "so the producer unwinds". `runProducer`'s own comment (`server_run_detach.go:144`) says it deliberately does not unwind. The comment on `append` is the stale half. |
| `internal/gateway/reserve.go:252` `(*Gateway).reserve` | the `error` result is always nil | Correct behaviour: a reservation-store failure becomes a `Deny` verdict, which fails closed. Only the signature is left over. |
| `internal/cron/recover.go:32` `(*Scheduler).recoverOrphans` | the `error` result is always nil | Its comment keeps it "to leave room for a future hard-fail policy". That is the speculative generality the repo rules forbid. |
| `cmd/aura/serve_governance_write.go:370` `buildMCPWriteProvider` | the `error` result is always nil | |
| `internal/share/bundle.go:89` `bundleArtifacts` | the `[]SnapshotArtifact` result: both production callers (`service.go:252`, `:355`) discard it | The projection is computed per artifact and thrown away. Only `bundle_test.go` reads it. |
| `internal/channels/telegram/bot_dispatch_turn.go:123` `startTurn` | parameter `to tele.Recipient` | |
| `internal/conversations/store_managed_history.go:232` `finishManagedHistory` | parameter `work *managedHistoryWork` | Both callers build `work` and use it before the call; the callee never reads it. |
| `internal/runner/runner_persist.go:174` `persistToolTurnEvent` | parameter `ev *agent.Event` | |
| `internal/runner/runner_persist_pause.go:28` `persistPause` | parameter `ctx` | |
| `internal/agui/governance_write_skills_api.go:231` `beginSkillsRead` | parameter `r` | |
| `internal/conversations/store_helpers.go:174` `validateID` | `kind` is always `"conversation_id"` (4 callers) | |
| `internal/documents/filecard/zip.go:171` `firstWords` | `limit` is always `maxProseWords` | |
| `internal/agent/tracing.go:53` `rootSpanIDs` | the `parent` result is always nil | Its comment states this by design (D-04). The result can go and the comment can stay. |

## Looked at, not findings

- **The `dupl` clusters at threshold 60** are mostly shape, not logic. Examples:
  - the three password-reset handlers (`internal/agui/password_reset_api.go:41-93`);
  - the skills restore, archive and delete handlers;
  - the three `must*` OTel instrument constructors (`internal/agent/metrics.go:229-257`, which are
    OTel's three typed constructors);
  - the per-store `withIdentity` wrappers around `db.WithIdentityTx`;
  - `approvalgrants.Revoke` against `approvalpolicies.Clear`.

  Folding these would need generics or a callback, which is more machinery than the lines it saves.
- **`deadcode` without `-test`: 40 entries.** 31 are in test-support packages (`agenttest`,
  `agenteval`, `steertest`, `dbtest`), which exist only for tests. Of the other 9:
  - `askuser.NewWithPauseTTL`, `mcp.AppsClientSettings` and `MCPAuditStore.List` with its
    `mcpAuditFromRow` are U1-U3 above.
  - `mcp.WaitForAbandonedCalls`, `obs.IsBoundedAttribute` and `reasoningfifo.FIFO.Len` are
    seams that tests in another package call:
    - `internal/agent/mcptools/mount_box_test.go`;
    - `internal/idempotency/telemetry_test.go`;
    - `cmd/aura/chat_render_reasoning_test.go`.

    They stay in production code because Go has no cross-package `_test.go`.
  - `garageadmin.WithHTTPClient` is **not** such a seam, and the cleanup missed it. Its only
    caller is the same package's `client_test.go:45`. With it go the `Option` type and the
    variadic `opts` on `New`; both production callers pass none (`cmd/aura/serve_provisioning.go:292`,
    `cmd/aura/document_processor_wiring.go:133`). The test is in-package and can set `c.hc`
    directly.
  - `retention.ratioFromCapacity` is a false positive: its caller is
    `disk_usage_windows.go`, which a Linux build excludes.
- **`unused` with `--tests=false`, `retention.ratioFromCapacity`:** the same Windows build-tag
  false positive.

## What this scan does not prove

- **Reachability is static.** `deadcode` and `unparam` reason over the call graph of a Linux
  build. Reflection, build tags (one Windows false positive is listed above) and calls from
  outside the module are not seen.
- **"No caller" means none in this repository.** An operator's own script, a bookmark, or an
  external integration calling one of the U6 routes would not show up. The routes may be used by
  hand, as the verification files show for two of them.
- **The duplicate search is lexical and AST-based.** Two functions that do the same thing in
  different code are found only if a name, a `dupl` window or a comment points to them. The
  UTF-8 truncation family was found that way, not by a tool.
- **U0 was measured on the serve handler, not on the lab VM.** The routing result does not depend
  on the deployment. The data-loss consequence follows from the code (`Deleter: s.run`), not from
  a live run.
- **The DB and env pass** is static too. A table that SQL outside this repository reads, such as
  an operator's own reporting query, would look unread here.
