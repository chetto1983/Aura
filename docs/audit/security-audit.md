# Security Audit — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`
**Scope:** `tools/*`, `mcptools/*`, `prompt/*`, `trust.go`, `hooks_command.go`, `llm_agent*`, with blast-radius into `internal/secret`, `internal/web`, `internal/swarm`.

## Threat model (must read first)

The runtime is designed (amendment #50 / D-15c) for a **single trusted operator on their own machine**: the host shell and filesystem *are* the capability, there is no sandbox, and there is no path fence. Therefore "the model can run any command / write any file" is the **intended capability, not a vulnerability**, and is excluded as such. This audit assesses security *within* that model plus the **deployment reality** that the same binary serves Telegram + AG-UI + scheduler from one daemon — which makes (a) cross-turn/cross-channel isolation and (b) multi-tenant readiness real concerns even though today's posture is single-operator.

**The one boundary the model crosses constantly is prompt injection:** untrusted bytes (web pages, files, MCP results, swarm child output) flowing into the model's context as if they were instructions. That is the primary security surface here, and the codebase mostly handles it well.

## 1. Prompt-injection surface

**Strong (confirmed good):**
- `trust.go` wraps untrusted tool output (web/fs/shell) in `<tool_output trust="untrusted" nonce=…>` with a **crypto-random nonce**, HTML-escaped + NFKC-normalized; the system prompt instructs the model to treat enveloped content as data, not instructions. This is a real, well-implemented mitigation.
- MCP server-provided `description`/`isError` text is recursively capped (512 B, depth-bounded) and framed "untrusted MCP server… treat as data" (`mcptools/bridge.go:170–261`).
- `document_search` stamps `TrustUntrusted` provenance explicitly.

**Gaps:**
- **AG-052 (P3 → P1 in multi-tenant): default-trusted for unknown tools.** `untrustedSource` (`trust.go:14–31`) is a hardcoded name set; a tool neither in the set nor setting provenance is treated as **trusted**. Critically, **`swarm_spawn` child reports are not marked untrusted** — a worker that fetched a malicious page returns content that, synthesized by the parent, is not enveloped. *(Relates to prior B-02.)* Fix: default unknown → untrusted; propagate untrusted provenance through swarm reports.
- **AG-003 (P1): command hooks can rewrite the model request/answer/tool-call** from stdout with no validation — a hook compromise is a prompt-injection-with-privileges vector.

## 2. Unsafe subprocess execution

- `shell_exec` running arbitrary commands is **intended** (trust model). Defenses that *do* exist and are good: secret-env stripping, process-group kill, `WaitDelay` orphan reaping, bounded output buffers, output secret-redaction, timeout cap.
- **AG-021 (P2, documented):** the destructive-command gate is advisory regex, trivially bypassable (`T=/; rm -rf $T`). Not a boundary — restated so it isn't over-credited.
- **AG-003 (P1): hook exec TOCTOU** — `verifyTrust` hashes the file (`hooks_command.go:206`) then `exec.CommandContext` re-opens it (`:182`); a writable hook path can be swapped between hash and exec. The `//nolint:gosec` asserts an atomicity the OS does not provide.

## 3. Unsafe file / network access

**Filesystem:** No fence by design. Within that: **AG-014 (P2)** no size cap on `fs_read/write/edit` (OOM); **AG-045 (P3)** non-atomic in-place writes (crash-truncate, parallel-edit race); **AG-019 (P2)** `send_file` fence silently disabled when `WorkspaceRoot==""` (active in prod). `send_file` itself has a *correct* symlink-resolving workspace fence — the one tool that genuinely confines.

**Network (web_fetch/web_search):** **Excellent SSRF hardening** (all confirmed in `internal/web`): http/https scheme allowlist; private/loopback/link-local/CGNAT/v6-metadata blocked; `169.254.169.254` blocked at IP **and** hostname layers; **pinned-IP dial with no resolve→dial TOCTOU**; manual per-hop redirect re-validation (auto-follow off, 5-hop cap); `io.LimitReader` size cap + content-type gate + timeout; structural error sanitization (IP/host/headers never reach the model). **AG-049 (P3):** no destination-port restriction (any port on a public IP). This layer is the security high point of the package.

## 4. Secret handling

- **AG-010 (P1): DB password leaks into `shell_exec` children.** `IsSecretEnvKey` substring denylist (`key,token,secret,pass,auth,bearer,credential,private,cert`) misses `AURA_DB_URL=postgres://u:PASS@h` and the `*_DSN/_URI/_CONN/_PWD` class. The model can `cat $AURA_DB_URL` and exfiltrate. Fix: DSN markers + value-scan redaction.
- **AG-003 (P1): command hooks inherit the full `os.Environ()`** — every provider/DB secret handed to a subprocess.
- **AG-009 (P1): reasoning-trace logs full prompts/history/PII**; redaction covers only named env-var secrets, not typed secrets or PII.
- **Good:** OTel never emits api_key (D-28); MCP child env is an allowlist; shell output is secret-redacted before the model sees it (just not DSN-shaped — AG-047).

## 5. Permission boundaries

- **AG-007 (P1): no per-call capability gate on mutating MCP tools.** Trust is binary at the server boundary; once mounted, any mutating tool runs unconditionally, and a reconnect can silently flip a tool to mutating (AG-024/F-8). The PRD's `capability_grants` (Slice 1.7) is not consulted in dispatch.
- **AG-011 (P1): skill self-activation is ungated** despite gated-looking comments/spec — the model can write executable instruction-skills that load into future system prompts without operator review. *(Matches prior B-04.)* Self-modification + injection-persistence surface.
- **AG-016 (P2): `agent_job` deferred execution** gated only by `rm/drop/delete` keywords — a benign-looking schedule fires an arbitrary full-tool agent turn later.

## 6. Injection (SQL/command/path) — within scope

- No SQL is built in this package (sqlc elsewhere). Path-join for the spillover sidecar is **safely** validated (`result.go validateID` restricts ids to `[A-Za-z0-9_-]` before `filepath.Join`; **AG-050** notes the un-asserted `runDir` invariant). Skill names validated `^[a-z0-9-]{1,64}$` before reaching the writer. Command "injection" into the shell is the intended capability.

## 7. Privilege escalation / persistence

- The deferred `agent_job` (AG-016) and ungated skill activation (AG-011) are the two **persistence** surfaces: an instruction that survives the current turn (a scheduled job) or survives across sessions (an activated skill) — both reachable by a prompt-injected instruction within a single benign-looking turn. These are the findings to revisit when the system moves beyond single-operator.

## Recommended mitigations (priority order)

1. **AG-007 / AG-011 — wire `capability_grants` into dispatch** for `Mutating && Untrusted` MCP tools and skill activation; default unknown-tool output to untrusted (AG-052) and propagate untrusted provenance through `swarm_spawn`.
2. **AG-003 — sandbox the hook surface:** minimal-env (no inherited secrets), exec-by-fd (close the TOCTOU), validate hook-supplied requests, audit every rewrite.
3. **AG-010 / AG-009 — close the secret boundary:** DSN-aware `IsSecretEnvKey` + value-scan redaction; don't log full history/PII to the trace by default.
4. **AG-016 — gate deferred `agent_job` schedules** (or surface the goal at fire time).
5. **Multi-tenant gate (future):** before any non-single-operator deployment, re-rate AG-003 (TOCTOU+env), AG-007, AG-011 as **P0**, and add a real sandbox/least-privilege runtime + production container (prior D-01).

> The injection-defense core (`trust.go` nonce envelope, SSRF hardening, MCP description capping) is genuinely strong and should be preserved as-is. The security debt is concentrated in **capability gating** and the **secret/hook boundary**, not in the model-context defenses.
