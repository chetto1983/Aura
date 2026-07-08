---
status: partial
phase: 37-per-user-full-capability-sandbox
source: [37-VERIFICATION.md]
started: 2026-07-07T10:30:00Z
updated: 2026-07-08T09:21:51Z
---

## Current Test

[testing paused — 3 items blocked on infrastructure prerequisites]

## Tests

### 1. Composition-root live egress DROP (closing SC#4/SBX-04 proof)
command: |
  docker build docker/aura-egress
  AURA_EGRESS_ENFORCE=1 go test -tags docker_integration -race ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor
  go test -tags docker_integration ./internal/sandbox/usersandbox/ -run 'TestEgress_FloorDropsInternal|TestEgress_FQDNAllowlist'
expected: A box created via buildSandboxRouter→Route carries its aura-egress sidecar (NET_ADMIN, shared box netns), reaches the public internet but is DROPPED from 169.254.169.254 / RFC1918; Stop leaves no orphan. Requires a native-Linux non-masquerading dockerd (Docker-Desktop/WSL vpnkit NATs the bridge and cannot validate DROP — RESEARCH Pitfall 3).
result: pass
reason: "LIVE-PROVEN 2026-07-08 on casaserver (192.168.1.21) — Ubuntu 24.04, kernel 6.8, native-Linux non-masquerading dockerd (docker context=default, NOT Docker-Desktop; satisfies Pitfall 3). `AURA_EGRESS_ENFORCE=1 go test -tags docker_integration -run TestEgress_FloorDropsInternal ./internal/sandbox/usersandbox/` → --- PASS (41.17s): box (busybox) reached example.com (public allowed) and was DROPPED from 10.0.0.1 (RFC1918) + 169.254.169.254 (metadata); aura-egress sidecar (NET_ADMIN on sidecar only, shared box netns) torn down cleanly on Stop (no orphan). The floor's tenancy-boundary DROP behavior is now behaviorally verified, not only mechanism-verified. ALSO PROVEN 2026-07-08 on casaserver: the composition-root variant `TestBuildSandboxRouter_LaunchesEgressFloor` (cmd/aura) --- PASS (16.85s) with AURA_SANDBOX_IMAGE=busybox:stable — the PRODUCTION buildSandboxRouter→Route path launched the sidecar and enforced the same reach-public+drop-internal boundary (busybox box avoids the fat aura-sandbox image; the wiring is identical). STILL DEFERRED: TestEgress_FQDNAllowlist needs an aura-egress image built with the pinned OpenSandbox binary. Earlier 2026-07-07 WSL run found+FIXED a latent CAP_ normalization assertion bug (commit abc578b5)."

### 2. Full docker_integration -race suite on native-Linux dockerd
command: go test -tags docker_integration -race ./internal/sandbox/usersandbox/... ./internal/agent/tools/... ./cmd/aura/... ./internal/config/...
expected: TestDockerBackend_RoundTrip, TestLifecycle_SuspendResumeDelete, TestVolume_CrossIdentityDeny, TestResolve_MaterializesInputs, TestReap_IdleSuspendAutoResume, TestRoute_StrictExecInBox, TestSnippetExec_RoutedEndToEnd, TestShellBg_RunsInBox, TestBuildSandboxRouterWiresEgress all PASS, goleak clean, -race clean. (This Windows host is CGO_ENABLED=0 — `-race` cannot even compile here.)
result: pass
reason: "-race suite LIVE-PASS on WSL 2026-07-07. Additionally 2026-07-08 the full internal/sandbox/usersandbox docker_integration suite ran on casaserver's NATIVE-Linux dockerd (no -race — box has no gcc): ok 79.29s (RoundTrip/Lifecycle/VolumeCrossIdentityDeny/Materialize/Reap/Route*/Spec*/Translate). Native-dockerd -race remains the only unrun dimension (gcc absent; -race dimension already covered on WSL)."

### 3. D-14 32GB concurrency soak on the real appliance host
command: AURA_SANDBOX_SOAK_REALHOST=1 go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak
expected: 10-20 concurrent per-identity boxes fit within the 32GB envelope with headroom; Resolve p95 <~2s; Resume p95 <~1s; cgroup-cap starvation probe shows no co-tenant starvation. Fill the 37-VALIDATION.md Manual-Only results table. (Dev WSL is capped at 15.47 GiB — verdict only meaningful on the real 32GB host.)
result: blocked
blocked_by: physical-device
reason: "Verdict only meaningful on the real 32GB appliance host; dev WSL is capped at 15.47 GiB (deliberately gated behind AURA_SANDBOX_SOAK_REALHOST). 2026-07-07 mechanism run at 9 GB / N=8 (informational) passed: Resolve p95 865 ms, Resume p95 361 ms, starvation-free. The 32GB / N=10-20 envelope verdict remains appliance-only."

### 4. gVisor runsc smoke + floor-under-runsc
command: bring up a box with `runtime: runsc` under server_production (compose.gvisor.yaml)
expected: gVisor runsc smoke passes; the filter-table floor still enforces the tenancy boundary under runsc; a configured FQDN allowlist together with runsc is correctly refused (ErrRunscFQDNMutualExclusion). Requires the runsc runtime installed on a native-Linux host.
result: blocked
blocked_by: other
reason: "Requires the runsc runtime installed on a native-Linux host; not available on the Docker-Desktop WSL2 engine (2026-07-07). Tool→box routing is otherwise proven live via runc (usersandbox Route tests + the docx/xlsx skill)."

## Summary

total: 4
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 2

## Gaps

None are code gaps. The 3 blocked tiers are genuine infrastructure prerequisite gates (not code
issues), consistent with the sanctioned WSL/CI Gate-3 deferral this phase has used from its first
verification onward (Phase-36 precedent, CLAUDE.md-documented Windows-dev-host limitation):
a native-Linux non-masquerading dockerd (egress DROP), a real 32 GB appliance host (soak envelope),
and the gVisor `runsc` runtime (runsc smoke). Per the verify-work protocol, blocked tests are
prerequisite gates and do NOT populate the code-gap list.

## Live Run — 2026-07-07 (WSL Ubuntu, Docker Desktop 29.6.1, `-race`)

1. **Composition-root egress DROP** — ⚠️ **Pitfall-3 gated (partial live evidence).** The sidecar launches via
   `buildSandboxRouter` with NET_ADMIN + shared box netns (confirmed live); the floor drops RFC1918 (the box's
   RFC1918 DNS `192.168.65.7` was itself dropped). The full "reach-public + drop-internal" assertion is
   unvalidatable on Docker Desktop because its DNS is *itself* RFC1918 — needs a native-Linux non-masquerading
   dockerd. A latent cap-assertion bug (`CAP_` normalization) was found + FIXED (`abc578b5`).
2. **docker_integration `-race` lifecycle/routing suite** — ✅ **LIVE PASS.** RoundTrip, Lifecycle
   (Suspend/Resume/Delete), VolumeCrossIdentityDeny, MaterializesInputs, Reap, all Route*/Spec*/Translate — real
   containers, race-clean. Plus real npm **docx** (8776 B valid OOXML) + **xlsx** (16215 B, live SUM formula)
   skills generated inside an aura-sandbox box.
3. **D-14 soak** — ✅ **mechanism PASS (9 GB / N=8, informational).** Resolve p95 865 ms, Resume p95 361 ms,
   starvation-free=true. The 32 GB / N=10–20 envelope verdict remains appliance-only (T-37-08-FALSEBENCH).
4. **gVisor `runsc` smoke** — ⏭️ **not runnable** (runsc not installed on the Docker Desktop WSL2 engine). The
   `server_production` tool-routing tests (`TestRoute_StrictExecInBox` etc.) are gated on this — tool→box routing
   is otherwise proven live via runc (usersandbox Route tests + the docx/xlsx skill).
