---
phase: 08
slug: sandbox-2b-session-bound
status: draft
threats_open: 5
asvs_level: 1
created: 2026-06-03
---

# Phase 08 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register origin: `register_authored_at_plan_time: true` — consolidated from the
> eight `08-0N-PLAN.md` `<threat_model>` blocks (08-01..08-08) plus the 08-09 CI/live
> register. It EXTENDS `05-SECURITY.md` (Slice 2a) — the 2a trust boundaries and
> `T-05-*` dispositions carry forward; 2b adds the session-container lifecycle, the
> per-conversation workspace, and the host-side egress proxy.
>
> **threats_open is NOT prematurely zeroed.** The five block-on-high `mitigate`
> threats whose proof is the LIVE integration tier (symlink cascade, egress
> allowlist + the landmine-3 reachability spike, TTL live reap, the connect-re-enable
> posture, and the cross-conversation state isolation) are authored + compile-green
> but their *live* confirmation is the **08-09 Task-3 human-verify Gate-3 sign-off**.
> They flip to `closed` and `threats_open: 0` only after that live tier is green
> (mirroring 05-SECURITY's discipline of not declaring a boundary closed on an
> assumption). See **Audit Trail** + **Sign-Off**.

---

## Trust Boundaries (extends 05-SECURITY.md / AR-05-01)

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Aura ↔ session container (lifecycle) | `internal/sandbox/sessions_docker.go` shells `docker run/stop/rm/ps` (LookPath-gated, fixed argv) for CONTAINER LIFECYCLE only; execution stays HTTP over the sidecar (D-05). NEVER `/var/run/docker.sock`. | Container create/destroy commands; never the socket. |
| session container ↔ host workspace | `internal/sandbox/workspace.go` owns `<runDir>/conversations/<id>/workspace/` (bind-mounted RW as `/workspace`, owner 65532). All host walks are `os.Root`/openat2 `RESOLVE_BENEATH` confined; cleanup is a manual no-follow post-order cascade (`PurgeConversationDir`). | Files the untrusted sidecar writes into `/workspace`; a sidecar-planted symlink can NEVER redirect a host read/unlink outside the tree. |
| session container ↔ host egress proxy | `internal/sandbox/network.go` is a host-side CONNECT forward proxy (D-08). The session container `connect(2)`s to the proxy at the **bridge-gateway IP** (NOT container `127.0.0.1`); the proxy validates the target HOSTNAME (no MITM), deny-wins glob allowlist → resolve-then-pin via the `internal/web` export → opaque tunnel. | Egress requests (hostname-validated); only allowlisted hosts resolving to public IPs are tunneled. Empty allowlist = egressless (2a posture). |
| CI live runtime ↔ 2b boundary | the gating DinD job (`.github/workflows/sandbox.yml`) runs the real session containers + live egress + mutation; no-skip-as-green hard-fails under `$CI` when env is unset (verify execution, not PASS). | Live criterion evidence (persistence, cascade, reap, egress), mutation scores, coverage. |
| session container ↔ host proxy (live spike) | the egress-bridge reachability spike (`TestNetwork_PyPIAllowed`) is a LIVE test (criterion 4); a failure proves the seccomp/bridge posture wrong — fix the posture, not the test. | The single highest-risk unverified assumption (A2 HIGH). |

---

## Threat Register (T-08-* consolidated)

| Threat ID | Category | Component | Disposition | Mitigation (implementing file) | Status |
|-----------|----------|-----------|-------------|--------------------------------|--------|
| T-08-01-DOC | Repudiation | PRD/DECISIONS drift (5 amendments + 2 schema landmines) | mitigate | `08-01-PLAN.md` amends prd.md/DECISIONS.md/ROADMAP.md before any code; lands the egress-pivot doc FIRST (closes the AR-05-01-style "shipped silently" gap). | closed |
| T-08-01-SC | Tampering | npm/pip/cargo installs | accept | Doc-only plan — no manifest touched (mirrors T-05-01-SC). | closed |
| T-08-02-V5-FK | Tampering | sandbox_sessions FK type (landmine #1) | mitigate | `0008_sandbox_sessions.up.sql:17` `conversation_id uuid REFERENCES aura.conversations(id) ON DELETE CASCADE`; `TestMigration0008_SchemaRoundTrip` asserts the uuid type + cascade. | closed |
| T-08-02-V14-ROLE | Elevation of Privilege | DB role separation | mitigate | `0008_sandbox_sessions.up.sql:31-32` dual GRANT (aura_app DML-only, aura_migrate DDL); migration runs as aura_migrate (INFRA-01). | closed |
| T-08-02-DOS-CAP | Denial of Service | unbounded sessions/workspace | mitigate | `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS=5` + `AURA_SANDBOX_WORKSPACE_MAX_BYTES=100MiB` (`config.go:170-171`); `ErrSessionCapReached` enforced in `sessions.go:204` (no silent LRU). | closed |
| T-08-02-INFO-PRIVACY | Information Disclosure | privacy-mode bypass | mitigate | `PrivacyMode` field (`config.go:174`); fail-fast cross-check in `sessions.go:166-168` (`ErrPrivacyModeEgressDenied`). | closed |
| T-08-02-SC | Tampering | go/sql/pip installs | accept | sqlc-generated + pgx existing; no manifest change. | closed |
| T-08-03-INFO-TIER | Information Disclosure | risk underclassification | mitigate | `scoring.go:91-100` `onlyPyPI` matches ONLY pypi.org + files.pythonhosted.org; any other host → Risky → `GateRecommended` true; modifier table monotone (`bumpTier` UP-only, property-tested). | closed |
| T-08-03-SCOPE | Repudiation | scope creep into P10/P11 | mitigate | D-12 scope guard: `ComputeTaskTier`/`ComputeSkillTier` built + unit-tested but UNWIRED; only `ComputeSandboxTier` is consumed (08-08). | closed |
| T-08-03-SC | Tampering | go module installs | accept | stdlib + existing rapid test dep only. | closed |
| T-08-04-INFO-SSRF | Information Disclosure | duplicated/divergent SSRF logic | mitigate | `network.go:158-160` `newWebGuard` delegates to `web.NewDialGuard` (no copy of `classify`); `export_test.go` no-drift guard; dupl gate enforces single-source (08-DECISIONS-WAVE0 OQ2/A4). | closed |
| T-08-04-REGRESS | Tampering | export breaks shipped Slice-5 web tier | mitigate | full `internal/web` tier re-run after export (incl. `TestDNSRebind`); deep-refactor-on-touch kept the edit minimal. | closed |
| T-08-04-SC | Tampering | go module installs | accept | stdlib + existing deps; no new module. | closed |
| **T-08-05-EOP-SYMLINK** | Elevation/Tampering | workspace symlink escape (CVE-2026-39861 shape) | mitigate | `workspace.go:88-113` `PurgeConversationDir` os.Root no-follow post-order cascade; `purgeBeneath` (`:149-170`) `root.Remove` unlinks the LINK never the target; `WalkSize`/`sizeBeneath` Lstat-skip symlinks. **Live proof:** `TestWorkspace_SymlinkEscapeCascade_Live` (block-on-high). | **open — live-pending (Task 3)** |
| T-08-05-EOP-SOCKET | Elevation of Privilege | docker-lifecycle carve-out | mitigate | `sessions_docker.go:32-81` `docker run/stop/rm/ps` via exec.CommandContext, LookPath-gated, fixed argv, `//nolint:gosec // fixed argv, no socket`; NEVER the socket (D-05, extends T-05-03-EOP-SOCKET). | closed |
| T-08-05-EOP-RUNTIME | Elevation of Privilege | gVisor not inherited by ad-hoc docker run (Pitfall 6) | mitigate | `sessions.go:238-268` `runArgv` passes `--runtime=<cfg.SandboxRuntime>` + ALL 2a hardening flags (user 65532, read-only, tmpfs, cap-drop ALL, no-new-privileges, seccomp, pids/mem/cpu/nofile) — extends T-05-02-EOP-RUNTIME to the per-session container. | closed |
| **T-08-05-DOS-CAP** | Denial of Service | session/RAM exhaustion | mitigate | hard cap 5 → `ErrSessionCapReached` (`sessions.go:204`, no LRU) + per-container 512m/1cpu/64pids in argv + 1800s TTL reaper (`sessions_reaper.go`). **Live reap proof:** `TestReaper_LiveContainerRemoved` (block-on-high). | **open — live-pending (Task 3)** |
| T-08-05-DOS-QUOTA | Denial of Service | workspace fills host disk | mitigate | `workspace.go:50-80` `WalkSize`/`CheckQuota` vs `AURA_SANDBOX_WORKSPACE_MAX_BYTES` → `ErrWorkspaceQuotaExceeded`. | closed |
| **T-08-05-INFO-XCONV** | Information Disclosure | cross-conversation state leak | mitigate | one container + one workspace dir per conv (`sessions.go` map keyed by convID, D-04); reaper removes both on eviction. **Live proof:** `TestSessions_PythonStatePersists` asserts a fresh conv does NOT see `x` (block-on-high). | **open — live-pending (Task 3)** |
| T-08-05-INFO-PRIVACY | Information Disclosure | privacy-mode bypass | mitigate | `sessions.go:166-168` Acquire fails fast when local-only + non-empty allowlist (D-10). | closed |
| T-08-05-SC | Tampering | go module installs | accept | stdlib (os.Root, sync, synctest) + existing goleak/sqlc/pgx; no new module. | closed |
| **T-08-06-INFO-EXFIL** | Information Disclosure | egress allowlist bypass / data-exfil (Claude Code bypass lesson) | mitigate | `network.go:243-287` CONNECT-time hostname validation + deny-wins glob (`policy.allow:111-124`) + resolve-then-pin (`:257-261`) + classify-every-fail-closed (Slice-5 export); per-session policy keyed by convID. **The documented Claude-Code-bypass caveat is recorded below (D-10 note).** **Live proof:** `TestNetwork_NonAllowlistRefused` (block-on-high). | **open — live-pending (Task 3)** |
| T-08-06-INFO-REBIND | Information Disclosure | DNS rebinding | mitigate | `network.go:257-261` resolve-then-pin (per-CONNECT / 60s dnsPin TTL) reuses Slice-5 `validateAndPin`/`dnsPin`; rejects ANY blocked record (no cherry-pick). | closed |
| T-08-06-TAMPER-SMUGGLE | Tampering | request smuggling through the proxy | mitigate | `network.go:243-256` validates CONNECT host:port only, parses no body / forwards no request (opaque Hijack+io.Copy tunnel); malformed CONNECT rejected. | closed |
| T-08-06-EOP-IPTABLES | Elevation of Privilege | in-container firewall (iptables anti-pattern) | mitigate | egress is host-side proxy ONLY (D-08); no `CAP_NET_ADMIN`, no in-container iptables (would contradict `cap_drop: ALL`). | closed |
| T-08-06-INFO-PRIVACY | Information Disclosure | privacy-mode bypass | mitigate | session-create fail-fast (`sessions.go`) prevents a non-empty allowlist under local-only; the proxy enforces only the policy it is handed. | closed |
| T-08-06-SC | Tampering | go module installs | accept | stdlib net/http (Hijacker) + the internal/web export; a stdlib suffix-match helper (`hostGlob`) avoids a globset dep (Package Legitimacy Gate). | closed |
| T-08-07-EOP-EXEC | Elevation of Privilege | persistent interpreter | mitigate | the interpreter runs inside the same hardened gVisor container (seccomp + cap_drop ALL + non-root 65532 + read-only rootfs from `runArgv`); sidecar stays stdlib-only (no IPython/ZMQ, D-03). | closed |
| T-08-07-INFO-XSESSION | Information Disclosure | cross-session namespace leak | mitigate | `sidecar.py:99-106` one namespace dict per `session_id` under `SESSIONS_LOCK`; one container per conversation (08-05). | closed |
| T-08-07-TAMPER-WIRE | Tampering | sidecar JSON response | mitigate | `docker.go:142-149` session path reuses the 2a `ErrSandboxProtocol` non-2xx/undecodable taxonomy + loopback-only port (extends T-05-03-V13-WIRE). | closed |
| T-08-07-DOS | Denial of Service | runaway session code | mitigate | reuses the 2a per-call timeout + 1 MiB stream truncation (`sidecar.py`) + container 512m/1cpu/64pids; idle interpreter bounded by the reaper. | closed |
| T-08-07-SC | Tampering | sidecar package / go module installs | accept | sidecar Python-stdlib-only (grep-enforced); docker.go stdlib net/http; no new module. | closed |
| **T-08-08-INFO-NET** | Information Disclosure | session container egress posture (the connect re-enable) | mitigate | session seccomp variant allows `connect` ONLY because egress is host-proxy-contained (08-06); proxy reachable at the BRIDGE GATEWAY IP (not container 127.0.0.1); empty allowlist keeps the 2a egressless posture (connect dead-ends). **Documented as a deviation EXTENDING AR-05-01 (below).** **Live reachability spike:** `TestNetwork_PyPIAllowed` (block-on-high, landmine #3). | **open — live-pending (Task 3)** |
| T-08-08-V5-INPUT | Tampering | session_id / network_allow tool args | mitigate | `workspace.go:208-221` `validateConvID` traversal guard on session_id; `network.go:88-106` `buildPolicy` parses + glob-validates network_allow CSV; a global `*` deny is rejected (`ErrGlobalDenyWildcard`). | closed |
| T-08-08-EOP-SYMLINK | Elevation/Tampering | cascade-on-delete | mitigate | `Conversations.Delete` routes through the os.Root no-follow `PurgeConversationDir` (08-05) instead of `os.RemoveAll` — closes the workspace symlink-escape on the host cascade path (covered live by T-08-05-EOP-SYMLINK proof). | closed |
| T-08-08-SCOPE | Repudiation | scope creep (D-12) | mitigate | only the sandbox advisory path is wired (`ComputeSandboxTier` → lean preview); NO scheduler pending_approval / agent_job_runs columns / skills pending dir. | closed |
| T-08-08-EOP-SOCKET | Elevation of Privilege | CLI terminate/prune lifecycle | mitigate | `sessions_control.go:70-113` terminate/prune drive docker stop/rm via the LookPath-gated fixed-argv dockerClient, never the socket (inherited T-05-03-EOP-SOCKET / D-05). | closed |
| T-08-08-SC | Tampering | go module installs | accept | stdlib + existing deps; seccomp-session.json derived from the shipped 2a profile (by-name syscalls, no new package). | closed |
| T-08-09-FALSEGREEN | Repudiation | integration / CI skip | mitigate | the `sandbox_integration` + `db_integration` helpers (`docker_integration_test.go:32-43` sidecarURL/sessionImage; `sessions_integration_test.go:42-53` envOrSkip; `network_integration_test.go` egressEnvOrSkip) `t.Fatal` under `$CI` when env unset; sub-second runtime is a skip tell; sandbox.yml exports the exact composed env (DSNs + AURA_SANDBOX_*). | closed |
| T-08-09-MUTSCORE | Repudiation | test adequacy on critical files | mitigate | `sandbox.yml` go-mutesting hard-fails <70% killed on `network.go` + `scoring.go` + `sessions.go` (security/decision-critical) under `$CI`; snapshot records the score. | open — CI-pending (Task 3) |
| T-08-09-AR0501 | Repudiation | egress posture documentation gap | mitigate | AR-05-01 is EXTENDED below for the 2b session-container connect-allowed posture (not silently shipped — 08-01 landed the egress pivot in the PRD first; this register records the live posture), closing the non-repudiation gap the 2a audit flagged. | closed (doc); live posture confirm at Task 3 |
| T-08-09-SC | Tampering | runsc/qemu/image/go-module installs | accept | all CI installs from approved 2a sources (runsc Google GPG apt, qemu-user-static Debian apt, python:3.12-slim Docker Official, go-mutesting Makefile-pinned); no new app dependency. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

**Totals:** 40 threats · 33 mitigate · 7 accept. **Open: 5** (the block-on-high `mitigate` threats whose LIVE confirmation is the 08-09 Task-3 Gate-3 sign-off: T-08-05-EOP-SYMLINK, T-08-05-DOS-CAP, T-08-05-INFO-XCONV, T-08-06-INFO-EXFIL, T-08-08-INFO-NET). T-08-09-MUTSCORE + T-08-09-AR0501-live are CI/live-pending (tracked, not counted in the 5 block-on-high). `threats_open` flips to 0 ONLY after the live tier is green.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-05-01 (EXTENDED for 2b) | T-08-08-INFO-NET / T-05-02-INFO-NET | See the dedicated extension section below. | davide marchetto (2a); pending live sign-off (2b) | 2026-06-02 / 2026-06-03 |
| AR-08-01 | T-08-09-SC (Claude-Code allowlist-bypass class) | The host-side egress allowlist (D-08) is a hostname-CONNECT control: it constrains WHICH hosts a session may reach, not WHAT data leaves to an allowed host. A determined exfil to an allowlisted host (e.g. encoding data into a pypi.org request path, or the documented "Claude Code allowlist bypass" class where an allowed CDN fronts arbitrary content) is NOT fully prevented by a hostname allowlist. Accepted-with-mitigation: (a) the DEFAULT posture is egressless (empty allowlist → no egress at all); (b) a non-empty allowlist is operator-opt-in and risk-scored Risky (`ComputeSandboxTier`) so it surfaces a gate recommendation; (c) under `AURA_PRIVACY_MODE=local-only` a non-empty allowlist FAILS FAST (D-10). The residual data-shape exfil to an explicitly-allowed host is accepted for v1 — the threat model is "untrusted code cannot reach arbitrary hosts", not "untrusted code with an operator-granted pypi allowlist cannot encode bytes into a pypi request". | davide marchetto | 2026-06-03 |

*Accepted risks do not resurface in future audit runs.*

### AR-05-01 — EXTENSION for the 2b session-container egress posture

The 2a accepted risk AR-05-01 documented the egress control deviation for the
STATELESS sidecar: egress blocked by `connect(2)`-denial in seccomp + a
non-masquerading `aura-sandbox-egressless` bridge (live bench S05 = 0.0% escape).
Slice 2b's host-side forward proxy (D-08) **requires the session container to
`connect(2)` to the proxy** — directly conflicting with the 2a connect-denied
profile (RESEARCH Pitfall 1 / landmine #3, 08-DECISIONS-WAVE0 OQ1/A2). The extension:

1. **Session containers run a connect-ALLOWING seccomp variant** (`sandbox/seccomp-session.json`,
   derived by-name from the 2a profile + `connect`). This is NOT a regression of the
   2a posture: egress is NOW contained **host-side by the forward proxy** (hostname-CONNECT
   allowlist + resolve-then-pin), not by connect-denial. The stateless 2a egressless
   profile remains the default for non-session calls.
2. **The proxy is reachable at the BRIDGE GATEWAY IP**, never container-local `127.0.0.1`.
   A loopback-published host port is unreachable from inside the container netns; the
   container reaches the host across the bridge gateway. The sidecar's
   `HTTP_PROXY`/`HTTPS_PROXY` env point at `<bridge-gateway-ip>:<proxy-port>`
   (`AURA_SANDBOX_PROXY_ENV`, injected by `runArgv` only when an allowlist is set).
3. **An empty allowlist keeps the 2a egressless posture** — backwards-compatible with the
   2a deny-totale: no proxy env, connect dead-ends. The connect-allowed variant + proxy
   route activate ONLY when `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` is non-empty.
4. **The deviation is documented HERE, not shipped silently** (the AR-05-01 reconciliation
   discipline): 08-01 landed the egress pivot in the PRD/DECISIONS FIRST (doc-gate), and
   this register records the shipped session egress control before the live confirmation.
5. **The live reachability spike is the gate** (A2 HIGH): `TestNetwork_PyPIAllowed` is the
   live proof the session container reaches the host proxy at the bridge gateway. If it
   fails, the posture is wrong — fix the posture, not the test. This is **08-09 Task-3
   live-pending**; the live result is recorded in `docs/aura-quality-snapshot.md` and this
   risk's status flips to confirmed only after the live green.

### Documented Claude-Code allowlist-bypass / data-exfil caveat (D-10)

Recorded as **AR-08-01** above: a hostname-CONNECT allowlist constrains reachable hosts,
not the byte-shape of what leaves to an allowed host. This is the "Claude Code allowlist
bypass" class (data smuggled into requests to an allowed host). Accepted-with-mitigation
for v1 (egressless default + Risky risk-score + privacy-mode fail-fast). NOT a silent gap.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-03 | 40 | 35 | 5 | register consolidated from 08-01..08-08 PLAN threat_models + 08-09 (planner) |

**Audit notes (2026-06-03):** The deterministic surface (08-02..08-08) is mitigated with
implementing-file citations; the five block-on-high `mitigate` threats whose proof is the
LIVE tier (symlink cascade, egress allowlist + reachability spike, TTL live reap, the
connect-re-enable posture, cross-conversation isolation) are authored + compile-green
(`-tags 'sandbox_integration db_integration'`, vet+build+test-compile exit 0) but remain
**open pending the 08-09 Task-3 human-verify Gate-3 sign-off** (live Docker + Postgres +
egress stack required — not available in the authoring env). AR-05-01 is EXTENDED for the
2b connect-allowed session posture (documented, not silently shipped); the Claude-Code
allowlist-bypass caveat is recorded as AR-08-01. `threats_open` is held at 5 — it flips to
0 only after the live tier is green (gsd-security-auditor re-audit at Gate-3).

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log (AR-05-01 extension + AR-08-01)
- [x] AR-05-01 EXTENDED for the 2b session-container connect-allowed egress posture
- [x] Claude-Code allowlist-bypass caveat recorded (AR-08-01)
- [ ] `threats_open: 0` — **NOT yet**: 5 block-on-high mitigate threats are live-pending the 08-09 Task-3 Gate-3 sign-off
- [ ] `status: verified` — **NOT yet**: flips after the live tier is green

**Approval:** PENDING live Gate-3 (08-09 Task 3). The deterministic register is complete
and every block-on-high threat has a concrete mitigation with an implementing-file
citation; the live confirmation closes the remaining 5.
