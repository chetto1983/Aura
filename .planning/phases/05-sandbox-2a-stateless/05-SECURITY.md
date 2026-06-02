---
phase: 05
slug: sandbox-2a-stateless
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-02
---

# Phase 05 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register origin: `register_authored_at_plan_time: true` (4 PLAN `<threat_model>` blocks). Verified by `gsd-security-auditor` (opus) against the implementation, not re-derived.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Go runner ↔ sidecar | `internal/sandbox/docker.go` POSTs `{code,timeout_sec}` over loopback `127.0.0.1:18901` to the in-container stdlib `http.server` (`sandbox/sidecar.py`). | Arbitrary user code (untrusted) in; stdout/stderr/exit_code/limit_hit out. Isolation, not validation, is the control. |
| Sidecar ↔ host kernel | User code runs as `subprocess.run` inside the hardened container; the kernel boundary is enforced by seccomp (`SCMP_ACT_ERRNO` default), `cap_drop: ALL`, `no-new-privileges`, non-root `65532`, userns-remap, and the gVisor `runsc` overlay (x86 default-on). | Syscalls (filtered); no host FS (read-only rootfs + tmpfs `/tmp` only); no egress (connect denied + non-masquerading bridge). |
| Operator ↔ runtime selection | `make sandbox-up` selects the runtime profile by arch (gVisor on x86, runc+seccomp floor on arm64). | Runtime hardening level. |
| CI ↔ live hardened runtime | `scripts/sandbox_escape_bench.sh` + tagged integration tests run the real boundary; no-skip-as-green hard-fails under `$CI`. | Escape-rate evidence, mutation score, userns assertion. |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-05-01-DOC | Repudiation | PRD/DECISIONS drift | mitigate | `05-01-PLAN.md` Tasks 1–3 cite D-05/06/07/09/20 + amendments #36/#37; microVM rejection preserved (doc-only). | closed |
| T-05-01-SC | Tampering | npm/pip/cargo installs | accept | `05-01-PLAN.md` modifies prd.md/DECISIONS.md/ROADMAP.md only — no package manifest; "no installs" holds. | closed |
| T-05-02-EOP-SYSCALL | Elevation of Privilege | seccomp.json allowlist | mitigate | `seccomp.json:3` `SCMP_ACT_ERRNO` default; ptrace/unshare/bpf/mount/process_vm_readv/kexec_load/userfaultfd absent from ALLOW; proven by `TestRunner_PtraceBlocked`/`UnshareBlocked` + bench S01/S02/S08–11 (0.0% escape). | closed |
| T-05-02-EOP-ROOT | Elevation of Privilege | container root = host root | mitigate | `compose.yaml:105` user 65532; `:111–114` `cap_drop: ALL` + `no-new-privileges:true`; daemon userns-remap asserted live `sandbox_escape_bench.sh:243` (D-15). | closed |
| T-05-02-EOP-RUNTIME | Elevation of Privilege | operator skips gVisor on x86 | mitigate | `Makefile:136–143` x86 appends `-f compose.gvisor.yaml` (runsc default-on); `compose.gvisor.yaml:19–20` (D-04). | closed |
| T-05-02-INFO-NET | Information Disclosure | network exfiltration | accept | **Shipped egress control deviates from the declared mechanism but is empirically sound.** Egress is blocked by (a) `connect` removed from the seccomp allowlist (`seccomp.json` `_comment`: "egress connect is denied by seccomp") and (b) a non-masquerading bridge `aura-sandbox-egressless` with `com.docker.network.bridge.enable_ip_masquerade:"false"` (`compose.yaml:139–143`) — **not** the register's declared `network_mode: none` + full socket-syscall subtraction. Live bench S05 egress probe = 0.0% escape. See **Accepted Risks Log AR-05-01** for the deviation rationale + reconciliation obligation. | closed |
| T-05-02-INFO-FILE | Information Disclosure | host file read | mitigate | `compose.yaml:106–108` `read_only: true` + tmpfs `/tmp`; no host bind mount; proven by `TestRunner_ProcRootDenied` + bench S06/S07/S14. | closed |
| T-05-02-DOS | Denial of Service | resource exhaustion | mitigate | `compose.yaml:116–120` `pids_limit:64`/`mem_limit:512m`/`cpus:1.0`/`nofile:64`; `sidecar.py:88–136` `limit_hit`; `TestRunner_TimeoutLimitHit`. | closed |
| T-05-02-ARM64 | Elevation of Privilege | gVisor arm64 non-GA | mitigate | `compose.gvisor.yaml:6–11` runsc x86-only; `Makefile:140–142` runc+seccomp floor on arm64; multi-arch by-name seccomp. | closed |
| T-05-02-SC | Tampering | apt/image installs | mitigate | `Dockerfile:11` digest-pinned `python:3.12-slim`; bash/coreutils from Debian apt; runsc from Google GPG-signed apt. | closed |
| T-05-02-SC-PIP | Tampering | curated pip set (D-20 bake) | mitigate | `requirements.txt` version+sha256 pinned; `Dockerfile:29` `pip install --require-hashes`; `:34` build-stage import smoke; runtime does no pip (net-contained + read_only). | closed |
| T-05-02-SECCOMP-FIT | Denial of Service | seccomp too tight for C-extensions | mitigate | `Dockerfile:34` baked-ok smoke + live `TestRunner_BakedPackagesImport` (sum==36) proves the floor is not over-tight. | closed |
| T-05-03-EOP-SOCKET | Elevation of Privilege | auto-start helper | mitigate | `docker.go:167–174` `autoStart()` gated on `exec.LookPath("docker")`; `:165` shells `compose up -d` only, never mounts the docker socket; bench C01=0. | closed |
| T-05-03-V5-INPUT | Tampering | execute lang/timeout args | mitigate | lang via `RunPython`/`RunShell` enum + `sidecar.py:48–51` 404 on unknown path; `timeout_sec` clamped ≤600 at both runner and sidecar. | closed |
| T-05-03-V13-WIRE | Tampering | sidecar JSON response | mitigate | `docker.go:120–127` non-2xx/undecodable → `ErrSandboxProtocol`; loopback-only port (`compose.yaml:121–122`); partial body never trusted. | closed |
| T-05-03-D18 | Repudiation | error taxonomy | mitigate | `errors.go:13–16`; `docker.go:128–135` non-zero exit → ToolResult (not Go error); integration tests assert environment-fault → typed sentinel (D-18). | closed |
| T-05-03-SC | Tampering | go module installs | mitigate | `docker.go` uses stdlib `net/http` + existing `go.uber.org/goleak` test dep only; no new module added. | closed |
| T-05-04-FALSEGREEN | Repudiation | bench / CI skip | mitigate | `sandbox_escape_bench.sh:58–76,263–271` exit non-zero under `$CI` when prereq absent; `docker_integration_test.go:35–42` `t.Fatal` under `$CI`; sub-second PASS treated as skip tell. | closed |
| T-05-04-USERNS | Elevation of Privilege | CI DinD userns-remap | mitigate | `sandbox_escape_bench.sh:243–255` asserts `name=userns` live, hard-fail under `$CI`; human-verify eyeballs `docker info`. | closed |
| T-05-04-ARM64 | Elevation of Privilege | QEMU arm64 divergence | accept | QEMU syscall emulation can diverge from a real arm64 kernel; accepted for 2a with the **tracked obligation** recorded in `docs/aura-quality-snapshot.md:99–103` + `05-VALIDATION.md:101` — real-DGX confirmation required before any production arm64 deployment (D-12). See **AR-05-02**. | closed |
| T-05-04-DENOM | Repudiation | escape-rate denominator | mitigate | `sandbox_escape_bench.sh:232–237` prints explicit N/A lines for inapplicable K01–K04 Kubernetes scenarios; auditable per-scenario table `:363–371` (OQ1) — no silent drops. | closed |
| T-05-04-MUTSCORE | Repudiation | docker.go test adequacy | mitigate | `sandbox_escape_bench.sh:263–296` go-mutesting on `docker.go` hard-fails <70% killed under `$CI`; snapshot records 100% killed. | closed |
| T-05-04-SC | Tampering | runsc/qemu/image installs | mitigate | All CI installs OS/container-level from approved sources (runsc Google GPG apt, qemu-user-static Debian apt, python:3.12-slim Docker Official); go-mutesting via Makefile-pinned module. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

**Totals:** 23 threats · 21 mitigate (closed) · 2 accept (closed) · **0 open**.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-05-01 | T-05-02-INFO-NET | The shipped egress control (`connect` removed from seccomp + non-masquerading `aura-sandbox-egressless` bridge) **diverges from the register's declared `network_mode: none` + full socket-syscall subtraction**. The substitution is engineering-necessary: a container with `network_mode: none` has no network namespace and cannot publish the loopback `127.0.0.1:18901` port the Go runner connects to, so the listener socket syscalls (`socket`/`bind`/`listen`/`accept`/`send*`/`recv*`) must remain allowed and the namespace must exist. Egress is instead contained by denying `connect(2)` in seccomp and removing the NAT route on the bridge. **Empirically validated:** live bench S05 egress probe = 0.0% escape (`docs/aura-quality-snapshot.md:95`). The risk accepted here is **not residual egress** (none measured) but the **auditability gap**: the deviation was shipped silently — `05-02-SUMMARY.md:99–101` states "Deviations from Plan: None functional" (incorrect), and the plan's own automated acceptance grep for `network_mode: none` (`05-02-PLAN.md:152`) would fail against the shipped `compose.yaml`. | davide marchetto | 2026-06-02 |
| AR-05-02 | T-05-04-ARM64 | gVisor `runsc` is x86-only and arm64 validation runs under QEMU user-mode emulation, whose syscall behaviour can diverge from a real arm64 kernel. Accepted for Slice 2a (x86 is the dev/prod target); a tracked obligation requires real-DGX arm64 confirmation before any production arm64 deployment (D-12), recorded in `docs/aura-quality-snapshot.md:99–103` and `05-VALIDATION.md:101`. | davide marchetto | 2026-06-02 |

*Accepted risks do not resurface in future audit runs.*

### Tracked obligation — AR-05-01 reconciliation (deferred, not done in this audit)

The egress control is sound, but the source-of-truth documents must be reconciled to remove the non-repudiation gap. Before this disposition is considered fully discharged:

1. **Amend the threat register** (`05-02-PLAN.md` T-05-02-INFO-NET) to describe the shipped control: `connect`-denial in seccomp + non-masquerading bridge, replacing the `network_mode: none` + full-socket-subtraction text.
2. **Amend `05-02-PLAN.md` truths/acceptance** (lines 27, 31, 101, 149, 152, 155) so the `network_mode: none` requirement and its acceptance grep match the shipped bridge design — with a cited decision ID (mirroring the D-09/D-20 amendment discipline) in `DECISIONS.md`.
3. **Correct `05-02-SUMMARY.md:99–101`** — the "Deviations from Plan: None functional" claim is false; record the network-namespace + socket-syscall substitution as a real (env/design-driven) deviation.

Until done, this remains an outstanding documentation obligation tracked here and in `docs/aura-quality-snapshot.md`.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-02 | 23 | 22 | 1 | gsd-security-auditor (opus) |
| 2026-06-02 | 23 | 23 | 0 | T-05-02-INFO-NET accepted (AR-05-01) by davide marchetto |

**Audit notes (2026-06-02):** Auditor verified 22/23 mitigations present in the implementation and flagged T-05-02-INFO-NET OPEN — the declared egress mitigation (`network_mode: none` + socket-syscall subtraction) does not match the shipped control (`connect`-denial + non-masquerading bridge), and the deviation was undocumented. Orchestrator independently confirmed the finding against `compose.yaml:139–143`, `sandbox/seccomp.json:349–368`, and `05-02-PLAN.md:152`. Disposition: **accept with tracked obligation** (AR-05-01) — egress empirically blocked (bench S05 0.0%), documentation reconciliation deferred.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-02 (with outstanding AR-05-01 documentation obligation)
