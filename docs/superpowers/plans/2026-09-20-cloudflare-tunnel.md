# Cloudflare Tunnel Remote Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one cockpit-managed Cloudflare Tunnel sidecar that exposes Aura through an Access-protected public hostname and a second WARP-required hostname, with all credentials encrypted in PostgreSQL.

**Architecture:** A typed Cloudflare API adapter feeds a resumable PostgreSQL-backed reconciler. Aura projects only the tunnel token into a private volume; a minimal non-root supervisor owns the pinned official `cloudflared` process. Both Cloudflare hostnames terminate at an internal Caddy listener that reuses the cockpit and Garage routes.

**Tech Stack:** Go 1.27, PostgreSQL 18, sqlc, golang-migrate, Cloudflare v4 API, Docker Compose, Caddy 2, React 19, TypeScript 7, TanStack Query, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-20-cloudflare-tunnel-design.md`

## Global Constraints

- PostgreSQL is the only durable authority for `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_TUNNEL_TOKEN`.
- Use a remotely managed tunnel. Quick Tunnels are forbidden because Aura requires SSE.
- Publish two hostnames: normal Access and Access + WARP posture. Create no private IP/CIDR route.
- Keep direct Caddy ingress on `0.0.0.0:443`; label it as Authula-only because it bypasses Cloudflare Access.
- The sidecar gets no database URL, API token, Docker socket, Aura secret or host port.
- Create Access before DNS. Use One-time PIN plus explicit active Aura email membership; never `Everyone` or unrestricted OTP.
- Mutate/delete Cloudflare resources only when persisted ID and Aura ownership marker both match.
- Cloudflare failure never makes Aura readiness fail or breaks direct Caddy access.
- At execution start re-read `internal/db/migrations/`. `0129` is the measured head; if `0130` exists, use the next free integer.
- Every touched Go package passes vet, tests and WSL race; coverage remains at least 85%; reconciler/supervisor mutation score is at least 70%.
- No source file may exceed 600 lines.

---

### Task 1: Persist remote-access state and implement the typed Cloudflare client

**Deliverable:** PostgreSQL holds encrypted credentials and resumable resource IDs; a fixture-tested client can manage accounts, zones, tunnels, DNS, OTP, Access and WARP posture without leaking credentials.

**Files:**
- Create: `internal/db/migrations/0130_cloudflare_remote_access.up.sql`
- Create: `internal/db/migrations/0130_cloudflare_remote_access.down.sql`
- Create: `internal/db/queries/cloudflare_remote_access.sql`
- Regenerate: `internal/db/sqlc/db.go`
- Regenerate: `internal/db/sqlc/models.go`
- Regenerate: `internal/db/sqlc/querier.go`
- Regenerate: `internal/db/sqlc/cloudflare_remote_access.sql.go`
- Modify: `internal/settings/settings.go`
- Modify: `internal/settings/settings_test.go`
- Create: `internal/cloudflareapi/client.go`
- Create: `internal/cloudflareapi/types.go`
- Create: `internal/cloudflareapi/account_zone.go`
- Create: `internal/cloudflareapi/tunnel_dns.go`
- Create: `internal/cloudflareapi/access.go`
- Create: `internal/cloudflareapi/client_test.go`
- Create: `internal/cloudflareapi/resources_test.go`
- Create: `internal/cloudflareapi/testdata/accounts-page-1.json`
- Create: `internal/cloudflareapi/testdata/accounts-page-2.json`
- Create: `internal/cloudflareapi/testdata/zone-pending.json`
- Create: `internal/cloudflareapi/testdata/tunnel-created.json`
- Create: `internal/cloudflareapi/testdata/dns-foreign.json`
- Create: `internal/cloudflareapi/testdata/otp-provider.json`
- Create: `internal/cloudflareapi/testdata/access-app.json`
- Create: `internal/cloudflareapi/testdata/posture-warp.json`
- Create: `internal/cloudflareapi/testdata/error-permission.json`
- Create: `internal/cloudflareapi/testdata/error-rate-limit.json`
- Create: `internal/remotetunnel/model.go`
- Create: `internal/remotetunnel/store.go`
- Create: `internal/remotetunnel/store_integration_test.go`
- Create: `scripts/cloudflare_remote_access_probe.sh`
- Create: `scripts/cloudflare_remote_access_probe_test.sh`
- Create: `docs/cloudflare-remote-access-contract.md`

**Interfaces:**
- Produces `cloudflareapi.Client` and `remotetunnel.Store` for Task 2.
- Secret settings: `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_TUNNEL_TOKEN`.

- [ ] **Step 1: Write failing encryption, persistence and HTTP contract tests**

```go
func TestCloudflareKeysAreSecret(t *testing.T) {
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"} {
		meta, ok := settings.Allowed(key)
		if !ok || !meta.Secret || meta.Kind != settings.KindString { t.Fatalf("%s = %#v", key, meta) }
	}
}

func TestListAccountsUsesBearerAndPaginates(t *testing.T) {
	server, requests := cloudflareFixture(t, fixturePage("accounts-1.json"), fixturePage("accounts-2.json"))
	client := cloudflareapi.New(server.URL, "secret-token", server.Client())
	got, err := client.ListAccounts(context.Background())
	if err != nil || len(got) != 2 { t.Fatalf("accounts=%d err=%v", len(got), err) }
	for _, r := range *requests {
		if r.Header.Get("Authorization") != "Bearer secret-token" { t.Fatal("missing bearer") }
	}
}

func TestPolicyNeverUsesEveryone(t *testing.T) {
	p, err := cloudflareapi.EmailPolicy("Aura users", []string{"Admin@Example.com"}, "otp-id")
	if err != nil { t.Fatal(err) }
	if strings.Contains(marshalJSON(t, p), "everyone") { t.Fatal("unsafe Everyone rule") }
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run: `go test ./internal/settings ./internal/cloudflareapi ./internal/remotetunnel`

Expected: FAIL because the packages, keys and schema do not exist.

- [ ] **Step 3: Measure and document the official API contract before implementation**

The probe calls `/user/tokens/verify`, `/accounts`, `/zones`, Access identity providers and posture endpoints with `Authorization: Bearer`, pipes responses through a redacting `jq` filter, and never enables shell tracing. Run it with credentials from a password manager; record envelope shapes, pagination and permission-denied codes without IDs, email, nameservers or tokens.

```bash
CLOUDFLARE_API_TOKEN='from-password-manager' \
CLOUDFLARE_TEST_ACCOUNT_ID='account-id' \
bash scripts/cloudflare_remote_access_probe.sh
```

- [ ] **Step 4: Add the singleton state migration and sqlc queries**

The table contains no secret values. It stores desired enablement/labels, generation, phase, account/zone/tunnel IDs, two DNS IDs, OTP provider ID, two Access application/policy IDs, WARP posture ID, health and sanitized error.

```sql
CREATE TABLE aura.cloudflare_remote_access (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  enabled boolean NOT NULL DEFAULT false,
  generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
  phase text NOT NULL DEFAULT 'disabled' CHECK (phase IN
    ('disabled','validating','waiting_nameservers','provisioning','connecting','healthy','degraded','error','deleting')),
  account_id text NOT NULL DEFAULT '', zone_id text NOT NULL DEFAULT '', zone_name text NOT NULL DEFAULT '',
  tunnel_id text NOT NULL DEFAULT '', tunnel_name text NOT NULL DEFAULT '',
  public_label text NOT NULL DEFAULT 'aura', warp_label text NOT NULL DEFAULT 'aura-warp',
  public_dns_id text NOT NULL DEFAULT '', warp_dns_id text NOT NULL DEFAULT '',
  otp_idp_id text NOT NULL DEFAULT '', public_app_id text NOT NULL DEFAULT '', public_policy_id text NOT NULL DEFAULT '',
  warp_app_id text NOT NULL DEFAULT '', warp_policy_id text NOT NULL DEFAULT '', warp_posture_id text NOT NULL DEFAULT '',
  last_error text NOT NULL DEFAULT '', observed_healthy boolean NOT NULL DEFAULT false,
  last_reconciled_at timestamptz, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_by text NOT NULL DEFAULT ''
);
INSERT INTO aura.cloudflare_remote_access (singleton) VALUES (true);
```

Queries use expected generation on every advance so stale reconcilers cannot overwrite newer intent.

- [ ] **Step 5: Implement strict domain types and Cloudflare client**

```go
type Phase string
type Desired struct { Enabled bool; ZoneName, PublicLabel, WARPLabel string }
type Resources struct {
	AccountID, ZoneID, TunnelID, PublicDNSID, WARPDNSID string
	OTPProviderID, PublicAppID, PublicPolicyID, WARPAppID, WARPPolicyID, WARPPostureID string
}
type State struct { Desired Desired; Generation int64; Phase Phase; Resources Resources; ObservedHealthy bool; LastError string }

type Secret string
func (Secret) String() string { return "[REDACTED]" }
func (s Secret) Reveal() string { return string(s) }
```

The client uses a 15-second timeout, a 2 MiB response cap, strict Cloudflare success envelopes and sanitized errors. It implements token verification, account/zone discovery/create, tunnel/token/config, CNAME ownership, OTP provider, self-hosted Access applications, explicit email policies and WARP posture. It classifies only 429, 5xx and transport timeouts as retryable.

- [ ] **Step 6: Run generation, DB integration and race gates**

```bash
make sqlc
go test ./internal/settings ./internal/cloudflareapi ./internal/remotetunnel
go test -tags db_integration ./internal/remotetunnel
bash scripts/sqlc_sync_gate.sh
wsl bash -lc 'cd /mnt/d/Repo/Aura && go test -race ./internal/cloudflareapi ./internal/remotetunnel'
```

Expected: PASS; raw credential rows start with `enc:v1:` and contain no plaintext.

- [ ] **Step 7: Commit the storage and client vertical slice**

```bash
git add internal/db internal/settings internal/cloudflareapi internal/remotetunnel scripts/cloudflare_remote_access_probe* docs/cloudflare-remote-access-contract.md
git commit -m "feat(remote-access): persist and control Cloudflare resources"
```

### Task 2: Reconcile the complete Cloudflare control plane safely

**Deliverable:** One idempotent service resumes from any persisted phase, publishes Access before DNS, synchronizes active Aura emails and deletes only Aura-owned resources.

**Files:**
- Create: `internal/remotetunnel/ports.go`
- Create: `internal/remotetunnel/reconciler.go`
- Create: `internal/remotetunnel/steps_zone_tunnel.go`
- Create: `internal/remotetunnel/steps_access_dns.go`
- Create: `internal/remotetunnel/members.go`
- Create: `internal/remotetunnel/delete.go`
- Create: `internal/remotetunnel/reconciler_test.go`
- Create: `internal/remotetunnel/delete_test.go`
- Create: `internal/remotetunnel/members_test.go`

**Interfaces:**
- Consumes: Task 1 client/store.
- Produces: `Reconciler.Reconcile`, `Disable`, `Delete`, `SyncMembers`, `Status`.

- [ ] **Step 1: Write failing transition, lockout and ownership tests**

```go
func TestReconcileResumesEveryPhase(t *testing.T) {
	for _, phase := range []Phase{PhaseValidating, PhaseWaitingNameservers, PhaseProvisioning, PhaseConnecting, PhaseDegraded} {
		t.Run(string(phase), func(t *testing.T) {
			store := stateStoreAt(phase); cloud := completeFakeCloudflare()
			r := New(store, cloud, fixedMembers("admin@example.com"), fakeProjection(), testLogger(t))
			if err := r.Reconcile(context.Background()); err != nil { t.Fatal(err) }
			assertNoDuplicateResources(t, cloud)
		})
	}
}

func TestDeleteRefusesForeignResource(t *testing.T) {
	r := reconcilerWithOwnershipMismatch()
	if err := r.Delete(context.Background(), "admin"); !errors.Is(err, ErrOwnershipConflict) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 2: Run tests and verify missing behavior**

Run: `go test ./internal/remotetunnel -run 'Reconcile|Delete|Members'`

Expected: FAIL.

- [ ] **Step 3: Implement narrow ports and exact reconciliation order**

```go
type Members interface { ActiveEmails(context.Context) ([]string, error) }
type Projection interface { Apply(context.Context, ProjectionState) error }
```

Order: validate token → account → zone → wait active nameservers → tunnel → OTP → public Access → WARP Access/posture → ingress → two CNAMEs → tunnel token → projection. Persist every create before the next external call. An empty active-email set or loss of the last administrator refuses publication.

- [ ] **Step 4: Implement retry, disable, deletion and drift semantics**

429/5xx/timeouts become degraded with bounded exponential backoff. Permission, validation and ownership failures become terminal error. Disable removes only the projection. Delete runs reverse dependency order, confirms absence before clearing IDs, preserves the zone and can resume midway.

- [ ] **Step 5: Run race and mutation gates**

```bash
go test ./internal/remotetunnel
wsl bash -lc 'cd /mnt/d/Repo/Aura && go test -race ./internal/remotetunnel'
wsl bash -lc 'cd /mnt/d/Repo/Aura && go-mutesting ./internal/remotetunnel/...'
```

Expected: PASS and at least 70% of reconciler/ownership mutants killed.

- [ ] **Step 6: Commit reconciliation**

```bash
git add internal/remotetunnel
git commit -m "feat(remote-access): reconcile Cloudflare access safely"
```

### Task 3: Build and package the supervised cloudflared sidecar

**Deliverable:** An idle-safe non-root sidecar hot-swaps tunnel tokens without Docker access; internal Caddy remains the single routing authority.

**Files:**
- Create: `internal/remotetunnel/projection.go`
- Create: `internal/remotetunnel/projection_test.go`
- Create: `internal/cloudflaresupervisor/supervisor.go`
- Create: `internal/cloudflaresupervisor/process.go`
- Create: `internal/cloudflaresupervisor/health.go`
- Create: `internal/cloudflaresupervisor/supervisor_test.go`
- Create: `cmd/aura-cloudflared-supervisor/main.go`
- Create: `cmd/aura-cloudflared-supervisor/main_test.go`
- Create: `docker/cloudflared/Dockerfile`
- Modify: `.dockerignore`
- Modify: `compose.yaml`
- Modify: `caddy/Caddyfile`
- Modify: `caddy/Caddyfile.domain`
- Modify: `.github/workflows/publish-aura-edge.yml`
- Modify: `scripts/install_env.sh`
- Modify: `scripts/install_lib_test.sh`
- Create: `scripts/cloudflared_image_contract_test.sh`
- Modify: `cmd/aura/container_artifacts_test.go`
- Modify after source commit: `scripts/payload_manifest.txt`

**Interfaces:**
- Projection: `/var/lib/aura/cloudflared/{token,desired.json}`.
- Sidecar: internal `GET /healthz`, `GET /status` on port 8085.
- Tunnel origin: `http://caddy:8080`.

- [ ] **Step 1: Write failing atomic projection and handoff tests**

```go
func TestProjectionIsOwnedBySidecarAndRemovedWhenDisabled(t *testing.T) {
	p := NewFileProjection(t.TempDir(), 65532, 65532)
	if err := p.Apply(context.Background(), ProjectionState{Enabled:true, Generation:7, Token:"secret"}); err != nil { t.Fatal(err) }
	assertModeOwner(t, p.TokenPath(), 0o600, 65532, 65532)
	if err := p.Apply(context.Background(), ProjectionState{Enabled:false, Generation:8}); err != nil { t.Fatal(err) }
	assertNotExists(t, p.TokenPath())
}

func TestFailedCandidateKeepsOldConnector(t *testing.T) {
	launcher := fakeLauncher(readyChild("old"), neverReadyChild("new"))
	s := NewSupervisor(testProjection(t, 1, "old"), launcher, Options{ReadyTimeout:100*time.Millisecond})
	runSupervisor(t, s); writeProjection(t, s.Root(), 2, "new"); waitState(t, s, "degraded")
	if launcher.Child(0).Stopped() { t.Fatal("healthy connector stopped") }
}
```

- [ ] **Step 2: Implement projection and supervisor**

Aura atomically writes/fsyncs/chowns the token to UID/GID 65532 with mode 0600; `desired.json` contains only enabled/generation. The supervisor starts a candidate with:

```text
cloudflared tunnel --no-autoupdate --protocol auto --metrics 127.0.0.1:<port> run --token-file /state/token
```

It waits for candidate readiness before terminating the old child. Idle is healthy. Status never exposes token, token hash or path.

- [ ] **Step 3: Pin and build the image**

Resolve the multi-arch digest with:

```bash
docker buildx imagetools inspect cloudflare/cloudflared:2026.8.3 --format '{{json .Manifest}}'
```

Use a multi-stage image containing the supervisor and pinned official binary, final UID 65532, CA certs and no shell. Docker health invokes `aura-cloudflared-supervisor healthcheck http://127.0.0.1:8085/healthz`.

- [ ] **Step 4: Add Compose and shared Caddy routing**

Refactor both Caddyfiles to import one `(aura_frontdoor)` snippet from external `:443` and unexposed `http://:8080`. Add `aura-cloudflared` with no host ports/socket/database network, read-only root, dropped capabilities, resource limits and `aura-cloudflared-state` mounted read-only; Aura mounts it read-write.

- [ ] **Step 5: Add image publishing/installer contracts and run tests**

Add edge image/pull-policy defaults and publishing workflow. Test idle health, UID, no shell, no ports and no forbidden environment.

```bash
go test ./internal/cloudflaresupervisor ./cmd/aura-cloudflared-supervisor ./cmd/aura
wsl bash -lc 'cd /mnt/d/Repo/Aura && go test -race ./internal/cloudflaresupervisor ./cmd/aura-cloudflared-supervisor'
bash scripts/cloudflared_image_contract_test.sh
bash scripts/install_lib_test.sh
```

- [ ] **Step 6: Commit sources, then regenerate the guarded payload manifest**

```bash
git add internal/remotetunnel internal/cloudflaresupervisor cmd/aura-cloudflared-supervisor docker/cloudflared .dockerignore compose.yaml caddy .github/workflows/publish-aura-edge.yml scripts/install_env.sh scripts/install_lib_test.sh scripts/cloudflared_image_contract_test.sh cmd/aura/container_artifacts_test.go
git commit -m "feat(remote-access): package supervised cloudflared sidecar"
make payload-manifest
bash scripts/payload_manifest_gate.sh
git add scripts/payload_manifest.txt
git commit -m "chore(payload): rehash Cloudflare sidecar configuration"
```

### Task 4: Wire the backend service, identity synchronization and admin API

**Deliverable:** Aura boots/rebuilds projection from PostgreSQL, reconciles in background, reacts to identity changes and exposes strict admin-only endpoints.

**Files:**
- Create: `cmd/aura/serve_remote_access.go`
- Create: `cmd/aura/serve_remote_access_test.go`
- Modify: `cmd/aura/serve_agui.go`
- Modify: `cmd/aura/serve_webui.go`
- Modify: `internal/agui/server.go`
- Create: `internal/agui/remote_access_api.go`
- Create: `internal/agui/remote_access_api_test.go`
- Create: `internal/agui/remote_access_api_authz_test.go`
- Modify: `internal/agui/onboarding_api.go`
- Modify: `internal/agui/onboarding_provision_test.go`
- Modify: `internal/agui/deprovision_route.go`
- Modify: `internal/agui/deprovision_route_test.go`
- Modify: `caddy/Caddyfile`
- Modify: `caddy/Caddyfile.domain`
- Modify: `cmd/aura/container_artifacts_test.go`
- Modify after source commit: `scripts/payload_manifest.txt`

**Interfaces:**
- Produces: `Server.SetRemoteAccess`, status/verify/configure/reconcile/token-refresh/external-acceptance/disable/delete/events endpoints.

- [ ] **Step 1: Write failing wiring, authorization and redaction tests**

```go
func TestRemoteAccessStatusNeverReturnsSecrets(t *testing.T) {
	s := remoteAccessServer(t, fakeRemoteAccess{apiToken:"api-secret", tunnelToken:"tunnel-secret"})
	rec := authenticatedRequest(t, s, http.MethodGet, "/api/settings/remote-access", nil, adminID)
	if rec.Code != http.StatusOK { t.Fatalf("status=%d", rec.Code) }
	if strings.Contains(rec.Body.String(), "secret") { t.Fatal("secret leaked") }
}

func TestRemoteAccessWriteRequiresAdmin(t *testing.T) {
	rec := remoteAccessWriteAs(t, memberID)
	if rec.Code != http.StatusForbidden { t.Fatalf("status=%d", rec.Code) }
}
```

- [ ] **Step 2: Implement composition root and coalesced background loop**

Build client/store/projection/members from existing settings and identity stores. Rebuild projection at boot. Wake reconciliation on startup, desired writes, identity changes and degraded/waiting timers; healthy drift-checks every five minutes. Coalesce wakeups and use `singleflight`.

- [ ] **Step 3: Add post-commit identity hooks**

Add a consumer-side `identityChanged func()` hook to successful onboarding/deprovision handlers. Invoke only after durable success, never inside an identity transaction and never on refused/compensated attempts. Tests assert one wake on success and none on failure.

- [ ] **Step 4: Implement strict admin endpoints**

```go
type remoteAccessDTO struct {
	Enabled bool `json:"enabled"`; Phase string `json:"phase"`
	PublicHostname string `json:"public_hostname,omitempty"`; WARPHostname string `json:"warp_hostname,omitempty"`
	APITokenSet bool `json:"api_token_set"`; TunnelTokenSet bool `json:"tunnel_token_set"`
	Connector string `json:"connector"`; LastError string `json:"last_error,omitempty"`; Generation int64 `json:"generation"`
}
```

Register dedicated routes before `PUT /api/settings/{key}`. Candidate-token verification does not store it. Final PUT stores only after validation. DELETE requires typed current hostname. Reuse the existing administrative capability check, strict body cap and idempotency middleware.

Candidate verification proves token activity and account enumeration; final configuration also
proves selected-account/zone readability before atomic storage. Cloudflare does not expose granted
scopes in token verification or a non-mutating proof of write permission. The first real
Tunnel/DNS/Access mutation therefore proves write access; refusal is a terminal sanitized status
and must never be reported as pre-verified success.

Authenticated acceptance is browser-driven: direct HTTPS listeners strip the internal marker and
only unexposed Caddy `:8080` sets it. The endpoint additionally requires the configured public Host,
current generation, current healthy owned connector and an Aura administrator. Persist the accepted
generation through existing observed-health state; never trust client-supplied Cloudflare headers or
introduce an Access service-token secret.

Cloudflare documents Dashboard rotation but exposes only a GET for the tunnel token in the public
API. Implement `token/refresh` to retrieve, encrypt and project the token after manual Dashboard
rotation. Do not call an undocumented endpoint or report that Aura itself rotated/revoked it.

- [ ] **Step 5: Run backend gates and commit**

```bash
go test ./internal/remotetunnel ./internal/agui ./cmd/aura
go vet ./internal/remotetunnel ./internal/agui ./cmd/aura
wsl bash -lc 'cd /mnt/d/Repo/Aura && go test -race ./internal/remotetunnel ./internal/agui ./cmd/aura'
git add cmd/aura internal/agui internal/remotetunnel caddy scripts/payload_manifest.txt
git commit -m "feat(remote-access): wire Cloudflare controls into Aura"
```

### Task 5: Build the complete cockpit onboarding and status surface

**Deliverable:** An administrator can configure, resume, observe, complete external acceptance,
refresh a Dashboard-rotated connector token, disable and delete Cloudflare remote access from
Settings in English or Italian.

**Files:**
- Create: `web/src/settings/remoteAccess/remoteAccessApi.ts`
- Create: `web/src/settings/remoteAccess/useRemoteAccess.ts`
- Create: `web/src/settings/remoteAccess/RemoteAccessPanel.tsx`
- Create: `web/src/settings/remoteAccess/RemoteAccessWizard.tsx`
- Create: `web/src/settings/remoteAccess/RemoteAccessStatus.tsx`
- Create: `web/src/settings/remoteAccess/TokenStep.tsx`
- Create: `web/src/settings/remoteAccess/ZoneStep.tsx`
- Create: `web/src/settings/remoteAccess/HostnamesStep.tsx`
- Create: `web/src/settings/remoteAccess/VerifyStep.tsx`
- Create: `web/src/settings/remoteAccess/__tests__/remoteAccessApi.test.ts`
- Create: `web/src/settings/remoteAccess/__tests__/RemoteAccessWizard.test.tsx`
- Create: `web/src/settings/remoteAccess/__tests__/RemoteAccessStatus.test.tsx`
- Create: `web/src/i18n/resources.remoteAccess.ts`
- Modify: `web/src/i18n/resources.ts`
- Modify: `web/src/settings/settingsSections.ts`
- Modify: `web/src/settings/SettingsWorkspace.tsx`

**Interfaces:**
- Consumes: Task 4 endpoints.
- Produces: `remote-access` admin settings section and resumable wizard.

- [ ] **Step 1: Write failing client and wizard tests**

```tsx
it('resumes at nameservers without asking for the token again', async () => {
  server.status({ phase:'waiting_nameservers', api_token_set:true, nameservers:['ada.ns.cloudflare.com','bob.ns.cloudflare.com'] });
  renderRemoteAccess();
  expect(await screen.findByText('ada.ns.cloudflare.com')).toBeTruthy();
  expect(screen.queryByLabelText('Cloudflare API token')).toBeNull();
});

it('sends typed hostname confirmation on delete', async () => {
  await deleteRemoteAccess('aura.example.com');
  expect(fetch).toHaveBeenCalledWith('/api/settings/remote-access', expect.objectContaining({
    method:'DELETE', body:JSON.stringify({hostname:'aura.example.com'}),
  }));
});
```

- [ ] **Step 2: Implement typed client and phase-driven polling**

Use discriminated types for every phase and shared `readJSON/httpErrorFrom`. Poll every two seconds only while transient, every 30 seconds healthy/degraded and never disabled. Invalidate `['settings','remote-access']` after mutations.

- [ ] **Step 3: Implement the resumable wizard and configured status**

Steps: Account → Domain → Nameservers → Tunnel → Access → WARP → Verify. Server phase selects the current step. Token must verify before saving. Domain purchase is explicitly external. Preview both URLs. Verify opens the public URL so the administrator completes Access OTP plus Authula before accepting the current generation. Status shows connector/reconcile health, both links, membership, direct-ingress bypass warning, retry, Dashboard rotation plus token refresh, disable and typed delete.

- [ ] **Step 4: Add bilingual resources and accessibility tests**

Put every string in `resources.remoteAccess.ts` and assert en/it key parity. Test one `h2`, labelled steps, status/alert roles, 44px destructive targets, focus return and no 390px overflow.

- [ ] **Step 5: Run frontend gates and commit**

```bash
cd web
npx vitest run src/settings/remoteAccess src/i18n
npm run typecheck
npm run lint
npm run contrast
npm run format:check
cd ..
git add web/src/settings/remoteAccess web/src/settings/settingsSections.ts web/src/settings/SettingsWorkspace.tsx web/src/i18n
git commit -m "feat(remote-access): configure Cloudflare from the cockpit"
```

### Task 6: Verify real external access, document operations and rebuild the cockpit

**Deliverable:** Hermetic UI E2E and a real Ubuntu Server/Cloudflare acceptance prove the public and WARP-required journeys, SSE, Garage and safe cleanup.

**Files:**
- Create: `web/e2e/remote-access.spec.ts`
- Create: `web/e2e/remote-access-live.spec.ts`
- Create: `scripts/cloudflare_tunnel_live_e2e.sh`
- Create: `docs/cloudflare-remote-access.md`
- Modify: `scripts/install.sh`
- Modify: `scripts/install_config_test.sh`
- Modify: `scripts/build_installer_test.sh`
- Modify: `web/playwright.config.ts`
- Modify: `README.md`
- Modify: `.env.example`
- Modify: `docs/asset-pipeline.md`
- Regenerate: `internal/webui/dist/**`

- [ ] **Step 1: Add hermetic wizard E2E**

Mock Cloudflare behind Aura's API. Cover token refusal, account choice, nameserver wait/resume after reload, configured state, direct-ingress warning, disable and typed delete. Assert token never appears in DOM, trace or request URL.

- [ ] **Step 2: Add fail-closed live-test preconditions**

Require `AURA_E2E_CLOUDFLARE=1`, API token, account ID, registered test zone and admin email. Under CI, opt-in plus missing value is fatal. Use unique `aura-e2e-<run>` labels and register cleanup before first mutation.

- [ ] **Step 3: Implement public and WARP-required live journeys**

Public: guided onboarding, OTP, Authula, incremental chat SSE, steer/cancel, Garage upload/finalize/download, Studio edit/save and Service Worker. WARP: enrolled client admitted, unenrolled denied. Assert no Aura-created private IP/CIDR route and no access to Postgres/Garage/MCP addresses.

- [ ] **Step 4: Prove token refresh, restart and ownership-safe cleanup**

Rotate the token through Cloudflare's documented Dashboard action, refresh it through the cockpit
during continuous public health polling, and record any unavoidable connector handoff interruption
truthfully. Restart Aura and sidecar separately and recover from PostgreSQL. Delete integration and
prove Aura resources vanish while the zone and a pre-seeded unrelated TXT record remain.

- [ ] **Step 5: Complete and test installer integration**

The repository installer and self-extracting appliance installer must ship the updated Compose and
Caddy payload, pull/start the published sidecar healthy-idle, and direct the operator to the cockpit
for Cloudflare onboarding. They must never prompt for or persist a Cloudflare credential. Add a
fresh-install plus rerun/update regression that proves PostgreSQL state and the projection volume are
preserved.

- [ ] **Step 6: Write operator documentation**

Document exact token permissions, registered-domain prerequisite, nameservers, OTP delivery, WARP enrollment, outbound firewall, direct 443 bypass, troubleshooting, PostgreSQL backup authority and disable/delete semantics. State that trusting a CA on Ubuntu does not trust remote browsers.

- [ ] **Step 7: Rebuild and run all gates**

```bash
cd web && npm test && npm run build && npm run typecheck && npm run lint && npm run contrast && npm run format:check
cd ..
go vet ./...
go build ./...
go test ./internal/cloudflareapi ./internal/remotetunnel ./internal/cloudflaresupervisor ./internal/agui ./cmd/aura ./cmd/aura-cloudflared-supervisor
wsl bash -lc 'cd /mnt/d/Repo/Aura && go test -race ./internal/cloudflareapi ./internal/remotetunnel ./internal/cloudflaresupervisor ./internal/agui ./cmd/aura ./cmd/aura-cloudflared-supervisor'
bash scripts/sqlc_sync_gate.sh
bash scripts/install_lib_test.sh
bash scripts/install_config_test.sh
bash scripts/build_installer_test.sh
AURA_E2E_CLOUDFLARE=1 bash scripts/cloudflare_tunnel_live_e2e.sh
```

Expected: all gates and thirteen live acceptance assertions pass; cleanup reports zero leaked Aura resources.

- [ ] **Step 8: Commit E2E, installer, docs and embedded UI**

```bash
git add web/e2e web/playwright.config.ts scripts/cloudflare_tunnel_live_e2e.sh scripts/install.sh scripts/install_config_test.sh scripts/build_installer_test.sh docs README.md .env.example internal/webui/dist
git commit -m "test(remote-access): verify Cloudflare Tunnel end to end"
```

### Task 7: Complete security and release review

**Deliverable:** Evidence proves secret containment, ownership-safe deletion, sidecar isolation and full release readiness.

**Files:**
- Create: `docs/audit/cloudflare-remote-access-review.md`
- Modify only for findings: files owned by Tasks 1–6.

- [ ] **Step 1: Audit both credential paths**

Trace browser write → encrypted row → API use / tunnel projection. Search source, built assets, `docker inspect`, argv, logs, traces, audit rows and raw PostgreSQL. Prove only `enc:v1:` ciphertext and the mode-0600 projection contain credentials.

- [ ] **Step 2: Audit ownership and network isolation**

Seed foreign same-name Cloudflare resources and require refusal. Inspect sidecar: UID 65532, read-only root, dropped capabilities, no ports/socket/database route. Confirm direct Caddy 443 still answers Authula.

- [ ] **Step 3: Run release-quality gates**

Run `make quality-full` in WSL with the real stack. Do not accept skipped integration tiers. Record combined coverage, race, mutation, live Cloudflare and cleanup results.

- [ ] **Step 4: Commit review evidence and any atomic fixes**

```bash
git add docs/audit/cloudflare-remote-access-review.md
git commit -m "docs(remote-access): record Cloudflare security verification"
```

## Execution Notes

- Execute tasks in order; each is one reviewer gate and one independently testable vertical result.
- Real account/domain credentials are optional for Task 1's read-only measurement and mandatory only for Task 6 completion.
- Never put Cloudflare credentials in `.env`; live tests receive them from the runner secret store only.
- If measured Cloudflare behavior differs, update the approved spec first and obtain review before changing public behavior.
