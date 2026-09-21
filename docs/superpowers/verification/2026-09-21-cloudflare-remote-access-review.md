# Cloudflare remote access — security and release review

Date: 2026-09-21. Reviewed tree: `a2f168989` (plan merge base `dc82b78f1`).
Scope: the Cloudflare Tunnel remote-access plan, Tasks 1–6
(`docs/superpowers/plans/2026-09-20-cloudflare-tunnel.md`).

Every claim below names the command that produced it. Claims that could not be measured are in
[What this review does not prove](#what-this-review-does-not-prove) — they are not summarised
anywhere else as if they had been.

## What this review does not prove

**The operator has no registered domain.** Cloudflare's named-tunnel, Access, Gateway and public
DNS surfaces cannot be exercised without one, and a Quick Tunnel is not a substitute (no SLA,
concurrent-request cap, no SSE — Aura chat cannot be certified on it). All **thirteen** live
acceptance assertions in `web/e2e/remote-access-live.spec.ts` are written, committed, executable
and **have not run**. Nothing in this document evidences that a public Cloudflare hostname serves
Aura.

| # | Blocked live assertion | Status |
|---|---|---|
| 1 | Pending-zone discovery, nameserver wait, resume after `aura` restart at that phase | not run |
| 2 | Owned CNAME + named tunnel with expected ingress, `warp-routing` disabled | not run |
| 3 | No Aura-created private IP/CIDR teamnet route; cloudflared reaches no private service | not run — **and see F-1** |
| 4 | Real Access OTP + Authula on the public hostname, `CF_Authorization` present | not run |
| 5 | Unlisted email submitted to Access and refused at the edge with no cookie | not run |
| 6 | Generation-bound external acceptance confirmed from the public tunnel only | not run |
| 7 | Incremental chat SSE, steer `202`, cancel 2xx, 16 s stream with zero errors | not run |
| 8 | Garage presign/PUT/finalize/download byte round-trip on the public origin | not run |
| 9 | Studio edit → save → library → differing downloaded bytes | not run |
| 10 | Gateway posture admits the enrolled client, denies the disconnected one | not run |
| 11 | API-token replacement + Dashboard connector rotation with 1 Hz public health sampling | not run |
| 12 | Separate `aura`/`aura-cloudflared` restarts recover; installer rerun preserves volumes | not run |
| 13 | Delete removes only owned resources against **real** Cloudflare inventory | not run — proved by code path + tests instead, see §2.3 |

Two further consequences, stated plainly:

- The **foreign-resource ownership refusal against real Cloudflare resources** (Step 2's seeded
  same-name resources) is blocked. §2.3 proves the guard **by its code path and its unit tests**,
  not against a live account. Those are different kinds of evidence and this document does not
  conflate them.
- Secret containment **during the live journeys** (`auditSecretContainment`) and the live
  `enc:v1:` PostgreSQL assertion inside the live suite are likewise unrun. The containment proof
  in §1 is measured on this host, on the shipped code and the shipped artifacts — it is not a
  recording of a real Cloudflare onboarding.

The harness is fail-closed and that was verified rather than assumed (Task 6): without
`AURA_E2E_CLOUDFLARE=1` it exits `2` with `BLOCKED`; opted in with a missing variable it exits `1`
naming the variable, before any network call; and `playwright.config.ts` does not even collect the
live spec unless the opt-in is set, so CI cannot report a green skip for acceptance that never ran.

---

## 1. Credential containment

Two credentials exist: the operator's **Cloudflare API token** (typed into the cockpit) and the
**tunnel connector token** (minted by Cloudflare, fetched by Aura). This section traces both from
browser write to final use.

### 1.1 The type system bounds the problem

`cloudflareapi.Secret` (`internal/cloudflareapi/types.go:10-28`) redacts every serialization route
— `String`, `GoString`, `Format`, `MarshalJSON`, `MarshalText` all return `[REDACTED]` — and
exposes exactly one accessor, `Reveal()`. `cloudflareapi.Client` carries its own `Format` returning
`cloudflareapi.Client{[REDACTED]}`, so a client value cannot be printed into a diagnostic either.

That makes the containment question finite: enumerate every `Reveal()`.

```console
$ grep -rn "\.Reveal()" --include=*.go . | grep -v "_test.go"
./cmd/aura/serve_remote_access.go:51:   if c.reconciler == nil || c.credential.Reveal() != token {
./cmd/aura/serve_remote_access.go:329:  _, err := a.secrets.Upsert(ctx, "CLOUDFLARE_TUNNEL_TOKEN", token.Reveal(), "")
./internal/cloudflareapi/client.go:98:  if strings.TrimSpace(c.token.Reveal()) == "" {
./internal/cloudflareapi/client.go:101: req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
./internal/remotetunnel/projection.go:38:       if state.Generation < 0 || (state.Enabled && state.Token.Reveal() == "") {
./internal/remotetunnel/projection.go:60:               if err := p.write("token", []byte(state.Token.Reveal()), 0o600); err != nil {
./internal/remotetunnel/reconciler.go:157:      if token.Reveal() == "" {
```

Seven sites. Four are emptiness or equality checks that emit nothing. **Three are destinations:**

| Destination | Site | Form |
|---|---|---|
| PostgreSQL `aura.settings` | `serve_remote_access.go:329` → `settings.Store.Upsert` | `enc:v1:` AES-GCM ciphertext |
| Cloudflare control plane | `client.go:101` | `Authorization: Bearer` over HTTPS to `api.cloudflare.com` |
| Sidecar handoff | `projection.go:60` | file mode `0600`, owner `65532:65532` |

No fourth destination exists, and a `[]byte`/`string` conversion that would bypass the redaction
appears nowhere outside those sites:

```console
$ grep -rn "string(.*[Tt]oken\|\[\]byte(.*[Tt]oken" --include=*.go \
    internal/remotetunnel/ internal/cloudflareapi/ internal/cloudflaresupervisor/ \
    cmd/aura/serve_remote_access*.go cmd/aura-cloudflared-supervisor/ | grep -v _test.go
internal/remotetunnel/projection.go:60: ... []byte(state.Token.Reveal()) ... 0o600 ...
cmd/aura/serve_remote_access.go:128:    ... string(state.Phase) ...          # a Phase, not a credential
```

### 1.2 Encryption at rest

`internal/settings/secrets.go` derives an AES-256-GCM key from `AURA_AUTHULA_SECRET` via
HKDF-SHA256 under the domain-separating info string `aura-settings-secret-v1`, and stores
`enc:v1:<nonce hex>:<ciphertext hex>`. An absent secret yields **no cipher at all** and
`ErrSecretsUnavailable` — `sealSecret` refuses rather than falling back to plaintext
(`secrets.go:57-70`), so a mis-provisioned deployment cannot silently store a credential in the
clear.

`internal/agui/settings_api_authz.go:54` additionally refuses both keys on the generic settings
API with `403 use the dedicated Remote access controls`, so neither can be written or read through
the ordinary settings surface.

**Measured against the live PostgreSQL** (read-only `SELECT`):

```console
$ docker exec -e PGPASSWORD=… aura-postgres psql -U aura -d aura -Atc \
  "SELECT key, is_secret, left(value,12), length(value) FROM aura.settings ORDER BY key;"
AURA_OPENROUTER_MANAGEMENT_KEY|t|enc:v1:c96eb|210
OPENROUTER_API_KEY|t|enc:v1:ea3a2|210
TELEGRAM_BOT_TOKEN|t|enc:v1:72ddd|156
…(non-secret rows omitted; is_secret=f)
```

Every `is_secret=t` row on the running deployment is `enc:v1:` ciphertext; there is not one
plaintext secret row. No `CLOUDFLARE_*` row exists — Remote Access has never been configured here,
which is consistent with there being no domain.

The two Cloudflare keys specifically are covered by an executable `db_integration` assertion that
reads the **raw column** through a real pgx pool
(`internal/remotetunnel/store_integration_test.go:83-99`):

```go
for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"} {
        secrets.Upsert(t.Context(), key, "fixture-credential", "")
        pool.QueryRow(t.Context(), "SELECT value FROM aura.settings WHERE key=$1", key).Scan(&raw)
        if !strings.HasPrefix(raw, "enc:v1:") || strings.Contains(raw, "fixture-credential") {
                t.Fatal("plaintext credential in database")
        }
```

`internal/settings/upsert_with_db_test.go:95-98` asserts the same for the transactional write the
cockpit actually uses. Both run in the `db_integration` tier measured in §3.

**The state table cannot hold a credential by construction.** `aura.cloudflare_remote_access` has
no credential column — only resource IDs, phase and timestamps:

```console
$ psql … -xc "SELECT * FROM aura.cloudflare_remote_access;"
singleton | t          phase          | disabled     public_label | aura
enabled   | f          account_id     |              warp_label   | aura-warp
generation| 0          …zone/tunnel/dns/app/policy/posture id columns, all empty…
last_error|            observed_healthy | f
```

### 1.3 Error text, logs and diagnostics

Three independent collapses stand between an upstream response and anything persisted or displayed:

1. **`cloudflareapi.APIError` never retains upstream message text.** It carries `Status`, `Code`,
   `Retryable` and a fixed internal `reason` (`client.go:44-58`). The response body is parsed for
   an error *code* only; its text is discarded.
2. **`Store.Advance` replaces arbitrary error text with a fixed diagnostic** before it reaches
   PostgreSQL — its own comment says why: *"response bodies and transport errors may contain
   credentials"* (`internal/remotetunnel/store.go:63-66`).
3. **`writeRemoteAccessError` never writes `err.Error()`.** It maps sentinel errors onto five
   fixed messages (`internal/agui/remote_access_api.go:202-216`), so no upstream text reaches an
   HTTP client at all.

Every log statement in the whole surface — `internal/remotetunnel`, `internal/cloudflareapi`,
`internal/cloudflaresupervisor`, `cmd/aura/serve_remote_access*.go`,
`cmd/aura-cloudflared-supervisor` — carries no error value and no credential:

```
reconciler.go:236        WarnContext(ctx, "Remote access reconciliation failed", "phase", s.Phase)
supervisor.go:131        Info("tunnel_lifecycle", "code", code, "generation", generation)
serve_remote_access.go:251  Warn("Remote access projection rebuild failed")
serve_remote_access.go:278  Warn("Remote access credentials unavailable; retrying")
serve_remote_access_wiring.go:22  Warn("Remote access settings unavailable")
remote_access_api.go:180 Info("aura admin: remote access change", "action", action, "actor", actor)
```

**Measured on the running deployment's logs:**

```console
$ docker logs aura --since 24h 2>&1 | grep -ciE "token[\"'=: ]+[A-Za-z0-9_-]{30,}"
0
$ docker logs aura --since 24h 2>&1 | grep -iE "cloudflare|remote access|tunnel"
(no output)
```

Caddy's own access log redacts the session cookie (`"Cookie":["REDACTED"]`, observed live).

### 1.4 Traces and audit rows

- **No HTTP request tracing exists.** `grep -rn "otelhttp" --include=*.go .` returns nothing
  repo-wide, and `internal/agui` contains no OpenTelemetry use at all, so no span can carry a
  request body.
- The one span that *does* wrap this path is the idempotency registry's, and it sets exactly three
  normalized enum attributes — operation, state, outcome (`internal/idempotency/telemetry.go:83-85`).
  No key, no fingerprint, no body.
- **The idempotency store persists a hash, not the request.** `normalizeHTTPMutation`
  (`internal/agui/idempotency_http.go:172-191`) builds the intent including the body, then
  `idempotency.FingerprintTyped(intent)` reduces it to `[32]byte`; only that goes to `Begin`. The
  table columns confirm it: `payload_hash bytea` for the request, `replay_body jsonb` for the
  *response*. The remote-access responses are `{"status":"accepted"}` / `RemoteAccessStatus`, which
  carries `api_token_set` / `tunnel_token_set` booleans and no value
  (`internal/agui/remote_access_api.go:16-33`).
- **No audit row carries a credential, because no audit row is written.** Neither
  `internal/settings` nor the remote-access surface writes `aura.audit_logs`; the only actor record
  is the `updated_by` identity UUID on the settings and state rows. On the live deployment
  `aura.audit_logs` holds 0 rows, and 0 rows match `%CLOUDFLARE%` or `(token|secret|api_key)` in
  `before`/`after`. See O-1 for the observation this implies.

### 1.5 The browser side

`web/src/settings/remoteAccess/` — measured, not asserted:

```console
$ grep -rn "localStorage\|sessionStorage\|console\.\|document.cookie\|URLSearchParams" \
    web/src/settings/remoteAccess/
(no output)
```

The token travels only as a JSON body field on `POST /token/verify` and `PUT /` with
`credentials: 'same-origin'` — never a query string (`remoteAccessApi.ts:70-102`). `useRemoteAccess`
deliberately omits the two secret-bearing operations from its TanStack mutations
(`useRemoteAccess.ts:37-49`), so no mutation cache retains them; the wizard clears its candidate on
successful configure **and** on unmount (`RemoteAccessWizard.tsx:41-52`).

**The shipped bundle, not the source.** The committed cockpit chunk that contains the Remote Access
surface is `internal/webui/dist/assets/SettingsWorkspace-C0FUlS-8.js` (77,751 bytes) at the
reviewed tree — the only chunk referencing `/api/settings/remote-access`, and it does contain the
cockpit (`git grep remote-access-heading HEAD -- internal/webui/dist` matches it, closing Task 6's
release-blocking concern #3). The concurrent video-studio cycle rebuilt the bundle afterwards
(`502a625ac`); the chunk is now `SettingsWorkspace-CFo4LPjY.js`, the same 77,751 bytes, and it was
re-scanned with the identical result — so this section holds for both:

```console
$ for p in localStorage sessionStorage indexedDB document.cookie console.log console.warn console.error; do
    printf '%-18s %s\n' "$p" "$(grep -o "$p" "$FILE" | wc -l)"; done
localStorage       0     sessionStorage     0     indexedDB          0
document.cookie    0     console.log        0     console.warn       0     console.error      0
```

In the built code `api_token` appears only as a request-body field —
`...n?{api_token:n}:{}` — and as the `api_token_set` boolean in render conditions.

### 1.6 The sidecar handoff — argv, environment, files

The connector token reaches `cloudflared` **as a file path, never as an argument value**
(`internal/cloudflaresupervisor/process.go:59`):

```go
cmd := exec.Command(binary, "tunnel", "--no-autoupdate", "--protocol", "auto",
        "--metrics", address, "run", "--token-file", token.Name())
cmd.Stdout, cmd.Stderr = io.Discard, io.Discard   // upstream diagnostics can contain credentials
cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/nonexistent"}
```

Each child gets a private immutable snapshot (`os.CreateTemp`, mode 0600, on the container's
1 MiB `tmpfs /tmp` owned `65532:65532`), removed when the child exits. cloudflared's own stdout and
stderr are discarded outright.

**Measured on a real container started from the shipped compose service:**

```console
$ docker top aura-cf-audit-aura-cloudflared-1 -eo pid,user,args
PID     USER    COMMAND
34677   65532   /usr/local/bin/aura-cloudflared-supervisor
$ docker inspect … --format '{{json .Config.Env}}'
["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
 "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt"]
$ docker logs aura-cf-audit-aura-cloudflared-1
(empty)
```

No credential in argv. **No credential-bearing environment variable at all** — the rendered compose
service declares no `environment:` key, and Remote Access deliberately adds no `.env` variable
(`7726be88e` reverted the `.env.example` change for exactly this reason; the live appliance's
`/opt/aura/.env` carries no `CLOUDFLARE_*` key). Swept across the whole running stack:

```console
$ for c in $(docker ps --format '{{.Names}}'); do
    docker inspect "$c" --format '{{range .Config.Env}}{{println .}}{{end}}' \
      | grep -E '^(CLOUDFLARE|TUNNEL_TOKEN)'; done
(no output — every running container on this host, zero matches)
```

The sidecar's own HTTP surface exposes no credential either — measured live from a probe container
on its network:

```console
$ wget -qO- http://aura-cloudflared:8085/status
{"state":"idle","generation":0,"active_generation":0,"ready":false}
```

The projection volume is mounted by exactly two services, and only one of them can write it:

```
aura:             aura-cloudflared-state -> /var/lib/aura/cloudflared   (read-write)
aura-cloudflared: aura-cloudflared-state -> /state                      read_only=True
```

**The projection files.** `FileProjection.Apply` writes `token` at `0600` owned `65532:65532`, and
`desired.json` at `0644`. The `0644` file is the one that could leak, and it cannot:
`ProjectionState.Token` is tagged `json:"-"` (`internal/remotetunnel/ports.go:26-30`), and
`projection_test.go:30-38` asserts the decoded object has **exactly two** fields (`enabled`,
`generation`) — a third field fails the test, which is stronger than a substring check.
`projection_linux_test.go:15-32` asserts the token file's uid, gid and `0600` by `syscall.Stat_t`.

### 1.7 Verdict on §1

**No credential appeared anywhere it should not.** Source, built assets, container environment,
argv, process table, container logs, daemon logs, spans, idempotency rows, audit rows and raw
PostgreSQL were all searched with the commands above. The only three places a credential value can
exist are the `enc:v1:` row, the outbound `Authorization` header, and the mode-0600 projection
file — and that is a property of the type system, not of reviewer diligence.

---

## 2. Ownership and network isolation

### 2.1 Sidecar hardening — measured, not read off compose

The image contract test runs a real container and checks the image itself:

```console
$ AURA_CLOUDFLARED_TEST_IMAGE=aura-cloudflared:task7-audit bash scripts/cloudflared_image_contract_test.sh
cloudflared version 2026.8.3 (built 2026-08-31-10:17 UTC)
ok: idle distroless sidecar is non-root, healthy, shell-free and unpublished without credentials or network
```

(The image was built from this tree: `docker build -f docker/cloudflared/Dockerfile -t aura-cloudflared:task7-audit .`)

Then the shipped compose service was started under a throwaway project and inspected:

| Property | Required | `docker inspect` |
|---|---|---|
| User | `65532:65532` | `User=65532:65532` ✅ |
| Root filesystem | read-only | `ReadonlyRootfs=true` ✅ |
| Capabilities | all dropped | `CapDrop=[ALL] CapAdd=[]` ✅ |
| Privilege escalation | blocked | `SecurityOpt=[no-new-privileges:true]`, `Privileged=false` ✅ |
| Process ceiling | bounded | `PidsLimit=64` ✅ |
| Published ports | none | `PortBindings=0`, `NetworkSettings.Ports=map[]` ✅ |
| Docker socket | absent | only mount is `aura-cloudflared-state → /state rw=false` ✅ |
| Networks | `aura-tunnel` only | `aura-cf-audit_aura-tunnel` (single entry) ✅ |
| Shell | absent | `/bin/sh` and `/bin/bash` both fail to exec ✅ |

The Docker socket is mounted into exactly one service across the whole rendered compose, and it is
not this one:

```console
$ docker compose --env-file <dummy> config --format json | (docker.sock mounts)
   aura  /var/run/docker.sock
```

### 2.2 Network reach — F-1

Only two services join `aura-tunnel`; every data-plane service is on `default` alone:

```
services attached to aura-tunnel:   aura-cloudflared [aura-tunnel]
                                    caddy            [aura-tunnel, default]
data-plane networks: postgres[default] arcadedb[default] garage[default]
                     aura[aura-web,default] aura-llama-embed[default]
                     arcadedb-mcp[default] aura-ingest[default]
```

So there is no DNS name and no compose-declared link from the sidecar to any database. That much
holds. **The stronger claim — that the sidecar therefore *cannot reach* PostgreSQL, Garage or the
MCP sidecar — does not hold on this Docker engine**, and assertion #3 probes exactly that. See
**F-1** below for the measurement, the mechanism and the remediation.

Caddy's private ingress port is correctly unpublished, and that half is confirmed by the same
probe: `172.18.0.15:8080` is **blocked** from another bridge. cloudflared reaches `caddy:8080`
over the shared `aura-tunnel` bridge (intra-network), which is the intended single bridge.

### 2.3 Ownership-safe deletion — proved by code path and tests, **not** against real resources

Step 2's seeded foreign same-name Cloudflare resources need a real account and domain and are
**blocked**. What was done instead, and it is a different kind of evidence:

Ownership is anchored on a tunnel name of the form `aura-<uuid>`, validated structurally
(`steps_zone_tunnel.go:89-95`), and **every** resource must carry that marker in the field
Cloudflare lets Aura own:

| Resource | Ownership predicate |
|---|---|
| Tunnel | `ownedTunnel`: id + `Name == aura-<uuid>` + `config_src == cloudflare` (legacy `remote_config` accepted) |
| DNS CNAME | `ownedDNS`: id + `Name == hostname` + **`Comment == tunnel name`** + `Type == CNAME` |
| Access application | id + `Name == <tunnel>-<role>` + `Type == self_hosted` + no unexpected child policy |
| Access policy | id + `Name == <tunnel>-<role>-members` |
| Gateway posture | `ownedPosture`: id + `Name == <tunnel>-gateway` + **`Description == tunnel name`** + `Type == gateway` |

`deleteResources` (`internal/remotetunnel/delete.go:139-152`) verifies **the entire persisted set
before removing anything** — its comment states the reason: *"so one foreign ID cannot trigger a
partial teardown"*. The zone and the account-wide OTP provider are never deleted, only detached. A
Cloudflare tombstone (`deleted_at`) is treated as absent, never as a deletable resource, and only
after ownership matches.

The refusal is exercised for all nine resource kinds, each seeded with a matching ID and a foreign
name, asserting `ErrOwnershipConflict` **and zero deletes**:

```console
$ go test -count=1 -v -run 'TestDeleteOwnershipGuardsEveryResource|TestDeleteRefusesForeignResource|TestDeleteConfirmsOwnedTombstoneAndRefusesForeignTombstone|TestOwnership' ./internal/remotetunnel/
--- PASS: TestDeleteRefusesForeignResource (0.05s)
--- PASS: TestDeleteConfirmsOwnedTombstoneAndRefusesForeignTombstone (0.00s)
--- PASS: TestDeleteOwnershipGuardsEveryResource (0.00s)
    tunnel  public-app  warp-app  public-policy  warp-policy
    gateway  public-dns  warp-dns  marker                       (9 subtests)
--- PASS: TestOwnershipRequiresPersistedIDAndAuraMarkerAgreement (0.00s)
--- PASS: TestOwnershipUsesAuthoritativeConfigSource (0.00s)
ok      github.com/chetto1983/aura/internal/remotetunnel 0.187s
```

This proves the guard against a fake Cloudflare. It does **not** prove it against real inventory —
assertion #13 remains blocked.

### 2.4 Direct Caddy 443 still answers Authula

```console
$ curl -sk -o /dev/null -w 'status=%{http_code}\n' https://localhost/
status=401
$ curl -sk -i https://localhost/api/settings/remote-access
HTTP/1.1 401 Unauthorized
Via: 1.1 Caddy
unauthorized
```

401 from Aura behind Caddy — no Cloudflare redirect, no `CF-Ray`, no Access interstitial. The
direct path is unchanged and still gated by Authula.

The ingress-marker boundary — the mechanism that makes external acceptance meaningful — was
verified against a **real Caddy container with `--network none`**, on both shipped frontdoors:

```console
$ AURA_CADDY_TEST_IMAGE=ghcr.io/chetto1983/aura-caddy:edge bash scripts/cloudflare_caddy_origin_test.sh
ok: Caddyfile tunnel and direct HTTPS preserve secure origin; spoofed downgrade and ingress marker refused
ok: Caddyfile.domain tunnel and direct HTTPS preserve secure origin; spoofed downgrade and ingress marker refused
```

That test asserts the tunnel frontdoor yields `https://remote.example.test|tunnel`, that a client
supplying `X-Aura-Remote-Ingress: forged` cannot influence it, and that the **direct** listener
yields `https://localhost|` — the marker stripped. A forged marker on direct 443 against the live
deployment is refused earlier still, at the Authula session gate (401, measured above).

---

## 3. Release gates

Run in WSL against the real container stack, at the Go tree of `a2f168989`. Two operator notes for
anyone reproducing this:

- **`make quality` must not carry database credentials.** It is the gate documented as needing no
  containers (`Makefile:139`). With `POSTGRES_PASSWORD` + `AURA_AUTHULA_SECRET` exported,
  `aura config show` reaches the operator's live database and reads its settings tier, and three
  `cmd/aura` config tests fail because that tier legitimately outranks their temp-`HOME` file tier.
  Deterministic, reproduced both ways; the numbers below are from a credential-free run.
- **`make coverage-docker`, not `make coverage`.** The `db_integration` tier `TRUNCATE`s shared
  auth tables; `coverage_gate.sh` refuses the live `aura` database (exit 5) and `coverage_docker.sh`
  provisions a disposable `aura_cov`, which is the same matrix on a database it is allowed to
  destroy. This is what Task 3's "authoritative isolated full matrix" also meant.

### 3.1 Owned-surface coverage — **PASS**

```console
$ make coverage-docker
==> provisioning disposable Postgres 'aura-postgres-cov' on 127.0.0.1:5433; removed on exit
==> migrating the schema into the disposable coverage DB
ok: 105 migration(s) applied
==> coverage gate: internal/* >= 85% (tags: db_integration)
total:                                          (statements)            88.1%
ok: owned_internal coverage 43041/48865 (88.1% displayed) >= 85%
ok: package-local coverage policy passed
```

**43041/48865 = 88.0814%** against the 85% floor — above Task 3's 42867/48691 = 88.0389%.
`artifacts/production-readiness/coverage-report.json`: `passed: true`, `tiers_executed:
["db_integration"]`, `empty_tiers: 0`, 82 packages evaluated, **74 at target**, 6 named
below-target baselines (`approvalgrants`, `assets`, `db`, `objectstore`, `objectstore/garageadmin`,
`webauth` — none of them this plan's), 2 delegated to their own authorities (`arcadedb`,
`sandbox/usersandbox`).

This plan's packages are all in `target` mode — the exact 85% floor, not a debt baseline — and all
clear it:

| Package | Mode | Covered/total | Percent |
|---|---|---|---|
| `internal/cloudflareapi` | target | 242/275 | 88.0000% |
| `internal/cloudflaresupervisor` | target | 196/205 | 95.6098% |
| `internal/remotetunnel` | target | 655/745 | 87.9195% |
| `internal/agui` (hosts the HTTP surface) | target | 7364/8279 | 88.9479% |

### 3.2 The integration tier really ran — no skip-as-green

`coverage_docker.sh` exports `CI=true`, so a tagged tier with missing env `t.Fatal`s instead of
skipping. That is the mechanism; here is the measurement. Runtimes of the integration-bearing
packages in this run:

```
cmd/aura 125.8s   internal/db 175.9s   internal/steer 59.2s   internal/agui 50.9s
internal/swarm 50.3s   internal/agent 17.4s   internal/breakglass 15.8s
internal/documents 14.8s   internal/cron 10.0s   internal/runner 7.8s …
```

Two packages finished sub-second — `internal/remotetunnel` 0.467s and `internal/settings` 0.506s —
which is the skip tell CLAUDE.md names, so they were re-run with `-v` against a fresh disposable
database rather than assumed:

```console
$ go test -tags db_integration -count=1 -v -run TestStorePersistsGenerationAndEncryptedCredentials ./internal/remotetunnel/
=== RUN   TestStorePersistsGenerationAndEncryptedCredentials
--- PASS: TestStorePersistsGenerationAndEncryptedCredentials (0.05s)
$ go test -tags db_integration -count=1 -v -run 'TestUpsertWithCommitsSecretAndDesiredStateAtomically|TestSecretRowsAreStoredEncrypted' ./internal/settings/
--- PASS: TestSecretRowsAreStoredEncrypted (0.07s)
--- PASS: TestUpsertWithCommitsSecretAndDesiredStateAtomically (0.09s)   [5 subtests]
```

`=== RUN` with `--- PASS` and **zero `--- SKIP`**: the two `enc:v1:` raw-row assertions §1.2 relies
on executed against a real PostgreSQL. Their packages are simply fast — a handful of SQL round
trips, not a skipped tier.

### 3.3 `make quality` — **PASS**

```console
$ make quality
QUALITY_RC=0
ok: quality gate passed (deadcode vet build file-size capability-declaration
    embedding-model-contract llm-model-contract lint test-race vuln)
```

| Step | Result |
|---|---|
| `deadcode -test` | clean — no unreachable Go |
| `go vet` (92 packages) | clean |
| `check-file-size` | all **3427** tracked source files within the 600-LOC cap |
| `check_capability_declaration` | PASS — 6 capability names, declared once |
| `fetch_embedding_model_test` | ok — EmbeddingGemma cache validates and refreshes atomically |
| `fetch_llm_model_test` | ok — both gemma-4-12B artifacts verify size+sha256 and stay commit-pinned |
| `golangci-lint` (incl. `dupl`) | clean |
| `go test -race` | **86 packages ok, 0 failures** — `cmd/aura` 158.9s, `internal/remotetunnel`, `internal/cloudflareapi`, `internal/cloudflaresupervisor`, `internal/agui` all ok |
| `govulncheck` | "Your code is affected by **0** vulnerabilities"; 0 in imported packages, 1 in a required module that the code does not call |
| `go build` | clean |

Neither of the two known Windows-host baseline failures
(`cmd/arcadedb-mcp.TestUnreachableJWKSIsReportedOnce`, `internal/idroot.TestContainedDirUnresolvableRoot`)
reproduces under WSL: both packages are `ok` in this run.

### 3.3.1 What the matrix actually measured, since the tree was not quiescent

A concurrent video-studio cycle was committing to `master` throughout. `git rev-parse HEAD` read
`a2f168989` when the run started and `9506cfe38` when it finished, so the honest statement is what
the diff says:

```console
$ git diff --stat a2f168989 9506cfe38 -- '*.go' go.mod go.sum compose.yaml caddy/ scripts/ docker/ internal/webui/dist/
(no output)
```

Zero changes to Go, module files, compose, Caddy, scripts, the sidecar Dockerfile or the bundle.
**The matrix measured exactly the Go and infrastructure tree this review audits.** The only
non-TypeScript commit since is `502a625ac`, the bundle rebuild handled in §1.5.

### 3.4 Gates outside `make quality-full`

| Gate | Result |
|---|---|
| `bash scripts/payload_manifest_gate.sh` | ok — payload matches its manifest (32 files) |
| `bash scripts/cloudflared_image_contract_test.sh` | ok — see §2.1 |
| `bash scripts/cloudflare_caddy_origin_test.sh` | ok on both frontdoors — see §2.4 |
| `npx vitest run src/settings/remoteAccess` | 4 files / 12 tests passed, re-measured at this HEAD |
| `npx tsc --noEmit` (web workspace) | clean |
| Mutation testing | **not run locally, by ruling** — it is measured in CI |
| Live Cloudflare + cleanup | **BLOCKED** — no registered domain; see the table at the top |

---

## Findings

### F-1 — A published container port is reachable from `aura-tunnel`, so "the sidecar cannot reach the database" is not delivered by network separation

**Severity: defense-in-depth.** No credential is exposed and no current control depends on this;
but live assertion #3 asserts it, and anyone reading "cloudflared joins only the tunnel network"
as "cloudflared is off the data plane" would be wrong.

Measured from a throwaway container on the tunnel network, against the running deployment:

```console
$ docker run --rm --network aura-cf-audit_aura-tunnel busybox:1.36 sh -c '…'
REACHABLE 172.18.0.8:5432      # postgres      (published 127.0.0.1:5432)
REACHABLE 172.18.0.6:3900      # garage        (published)
REACHABLE 172.18.0.14:8096     # arcadedb-mcp  (published)
REACHABLE 172.18.0.7:2480      # arcadedb      (published)
REACHABLE 172.18.0.15:443      # caddy 443     (published)
blocked    172.18.0.15:8080    # caddy private ingress (NOT published)
blocked    172.18.0.12:9080    # aura agui     (not published on this bridge)
blocked    172.18.0.250:5432   # control: no container
blocked    172.18.0.8:9999     # control: closed port
```

Not a false positive: a real PostgreSQL SSLRequest got a genuine `N` reply, and both controls were
refused. The probe network is the one the shipped compose creates — `aura-tunnel` is declared as a
plain `driver: bridge` with no other options (`compose.yaml:1606-1608`), so the throwaway project's
bridge is structurally identical to the appliance's.

**Mechanism.** Docker Engine 29.7.2's cross-bridge isolation is intact — the per-bridge drop rule
fired for the controls (`iifname != "br-913cf332874c" oifname "br-913cf332874c" … drop`, 6 packets).
But publishing a port installs an `accept` rule ahead of it that matches **any** input interface:

```
iifname != "br-913cf332874c" oifname "br-913cf332874c" ip daddr 172.18.0.8 tcp dport 5432 … accept   # 3 packets — this probe
```

Binding to `127.0.0.1` on the host does not narrow it. Every `ports:` entry in `compose.yaml`
therefore punches a hole through bridge isolation that any other Docker network on the host,
including `aura-tunnel`, can use.

**What this does and does not mean.** Reaching a TCP port is not access: the sidecar image has no
shell, drops all capabilities, runs read-only, holds no database credential and runs exactly one
statically-linked binary that execs cloudflared with discarded stdio. Exploiting this needs code
execution inside cloudflared *and* a database credential the sidecar has never held. It is a
missing layer, not an open door.

**Not fixed here, deliberately.** The two real remediations are both outside this task's scope and
neither is a one-line change: (a) stop publishing the data-plane ports, which is how the operator
and the local tooling reach the stack today and would be a large, risky appliance change; or (b)
install an explicit `DOCKER-USER` drop for traffic from the tunnel bridge to the default bridge,
which Compose cannot express and which belongs to the appliance installer. Weakening assertion #3
to match the current behaviour was rejected: the assertion is not wrong, the deployment simply does
not satisfy it.

**Consequence to plan for:** when a domain finally exists, `verifyTunnelNetwork()`
(`web/e2e/support/remoteAccessLive.ts:115-165`) will **fail** on its `nc -z` probes. That is the
test doing its job, and it should be treated as a known open item rather than a surprise.

**Engine caveat:** measured on Docker Desktop, Engine 29.7.2, WSL2 backend. The Ubuntu appliance
runs the same major engine with the same nftables layout, so the same result is expected — but it
has not been measured there. Running the probe block above on the appliance would settle it.

### O-1 — Observation: a Cloudflare credential change leaves no audit row

`aura.audit_logs` exists and has `before`/`after` jsonb, but neither `internal/settings` nor the
remote-access surface writes to it (0 rows on the live deployment). The only record of who changed
the API token is the `updated_by` identity UUID on the settings row, overwritten by the next write.
This is consistent with every other settings key and is **not** a credential leak — the absence of
a row is why nothing leaked there. Recorded because an operator reading "audit rows were searched"
should know the search found an empty table by design, not a clean one by luck.

### O-2 — Observation: `scripts/cloudflare_caddy_origin_test.sh` still has no CI caller

Carried from Task 6. It is the only executable proof of the ingress-marker boundary and the
forwarded-scheme fix, and it runs only when someone remembers it exists. It needs a built
`aura-caddy` image, which no CI job currently produces, so wiring it means adding an image-build
job. Ran manually above (both frontdoors green); flagged, not fixed.

---

## What would unblock the rest

A registered domain added to Cloudflare as a **pending** zone (pending is required — an active zone
cannot prove the nameserver wait/resume leg), a disposable Ubuntu appliance installed from the
generated self-extracting installer with Authula set up and Remote Access pristine, a second scoped
API token for the replacement leg, a pre-seeded unrelated TXT record, mailbox access for OTP, a
Cloudflare One organization whose client can be enrolled and disconnected on the browser machine,
and a funded chat model with working Garage/Studio. The twelve variables are listed in
`docs/cloudflare-remote-access.md`. Then, on an attended Linux desktop:

```bash
AURA_E2E_CLOUDFLARE=1 bash scripts/cloudflare_tunnel_live_e2e.sh
```

For F-1 specifically, no domain is needed: run the probe block from §F-1 on the Ubuntu appliance to
confirm or refute the engine caveat, then decide between the two remediations.
