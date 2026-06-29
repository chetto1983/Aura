---
phase: 29
slug: governance-write-mcp-configuration-skills-install
status: verified
threats_open: 0
asvs_level: 2
created: 2026-06-21
---

# Phase 29 — Security

> Per-phase security contract for the cockpit governance WRITE surface (MCP config
> install/env-edit/lifecycle + skills install/approval/restore), all behind the
> `governance.write` capability, with append-only audit ledgers. Threat register was
> authored at plan time (`register_authored_at_plan_time: true`); this audit VERIFIES
> each declared mitigation against the implemented code — evidence is a concrete
> grep/read match at the right location covering all entry points.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| cockpit browser → SSR mux → agui write handler | untrusted operator request crosses RequireAuth → RequireCapability(governance.write) | install/env/trust/lifecycle requests, skill source |
| app role (`aura_app`) → `aura.mcp_audit` / `aura.skill_audit` | the DB role boundary that makes both ledgers tamper-evident (SELECT+INSERT only) | append-only audit rows |
| write provider → managed `servers.json` | the FS write the atomic temp→tx→rename wrapper protects | MCP config doc |
| backend env DTO → API response / DOM | the boundary a raw secret must NEVER cross (key-only chips) | env KEY chips (no value) |
| skills.sh / source field → `npx skills` → staged tree | untrusted supply-chain content crosses into staging (Writer gate validates before pending) | fetched skill body |
| pending/ skill → LLM context | the boundary pending must NEVER cross (loader scans active/ only) | skill body |
| operator approval → ResumeHandler.Resume → Writer.Activate | the ONLY activation path (no model-facing approve) | activation decision |
| manager → MCP server process (probe) | the live tool-count probe (configured-servers-only, stdio) | initialize/tools-list |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation (verified control) | Status |
|-----------|----------|-----------|-------------|-------------------------------|--------|
| T-29-01-01 | Tampering | `aura.mcp_audit` ledger | mitigate | `0022_mcp_audit.up.sql`:35-58 — reject fn (42501) + `mcp_audit_no_update_delete` BEFORE UPDATE OR DELETE FOR EACH ROW + `mcp_audit_no_truncate` BEFORE TRUNCATE FOR EACH STATEMENT + `GRANT SELECT, INSERT … TO aura_app` (no U/D/T grant); live-PG `TestMCPAuditAppendOnly` (audit_store_integration_test.go:90-158) asserts 42501 on UPDATE/DELETE/TRUNCATE + rows survive | closed |
| T-29-01-02 | Repudiation | MCP config mutations | mitigate | `audit.go:87` `InsertMCPAuditTx(ctx, q *sqlc.Queries, …)` tx-bound; `configwrite.go:42-58` `WriteConfigWithAudit` runs it inside `db.WithTx`, `os.Rename` only on tx commit, `os.Remove(tmp)` on tx failure (all-or-nothing) | closed |
| T-29-01-03 | Elevation of Privilege | `governance.write` capability | mitigate | `serve_webui.go:114` const = `"governance.write"`; mounted on all 13 routes via `RequireCapability(…, governanceWriteCapability)` (serve_webui.go:383-402); `auth.go:261-278` RequireCapability→`HasCapability` (wildcard-or-exact, identity/store.go:125-128); `*` is system-managed — `validateGrantInput`/`GrantCapability` reject it (`ErrWildcardManaged`, identity/store.go:200-201) so no operator grants a capability they lack | closed |
| T-29-01-SC | Tampering | npm/pip/cargo installs (plan 01) | mitigate | 29-01 added no deps (migration + Go store + docs amendment only); `go.mod`/`web/package.json` unchanged by 29-01 | closed |
| T-29-01-04 | Information Disclosure | `mcp_audit.reason` free-text | **accept** | env-edit never routes a value into the audit reason: `serve_governance_write.go` sets `MCPAuditInsert.Reason` ONLY on `trust` (line 122, operator-authored note); install/edit/enable/disable/remove omit Reason (lines 58-59, 83-84, 145-146, 166-167). Surfaced only behind governance.read/write. Recorded in the Accepted Risks Log below | closed |
| T-29-02-01 | Information Disclosure | env values in responses/logs | mitigate | `governance_write_api.go:196-213` `mcpWriteResponseFrom` projects env via `envChips` (key-only `{key, redacted}`, governance_api.go:209-221 — no value field) on every env-bearing response; probe `Detail`/`Err` re-sanitized; held-out `TestGovernanceWriteSecretScan` (governance_write_secret_scan_test.go) proves zero secret values over a full run | closed |
| T-29-02-02 | Tampering | unchanged-secret overwrite on edit | mitigate | `envedit.go:53-93` `mergeSubmittedEnv` preserves the stored secret when submitted == redacted `${KEY}` placeholder (line 75, `isPlaceholderValue` + `secret.IsSecretEnvKey`); rapid property `TestSetServerEnvPreservesAllSecrets` (envedit_property_test.go) holds for ALL secret keys + a rotate branch (non-degenerate) | closed |
| T-29-02-03 | Elevation of Privilege | model self-trusts a server | mitigate | trust route mounted at `governanceMCPTrustRoute` behind `RequireCapability(governance.write)` (serve_webui.go:385); `serve_governance_write.go:96-127` populates ApprovedBy from the principal; custom servers default to `TrustBlocked` (line 228) — no model-facing trust path exists | closed |
| T-29-02-04 | Repudiation | unaudited config mutation | mitigate | `configwrite.go:42-58` makes the config write + `InsertMCPAuditTx` all-or-nothing (temp→tx→rename); one audit row per named action (six adapter methods each pass a single `MCPAuditInsert`) | closed |
| T-29-02-05 | Tampering | denied/destructive tool silently mounted | mitigate | `denied_mount_test.go` `TestDeniedToolNotMounted` proves a TrustBlocked server is absent from `RunnableManagedServers`/`RuntimeServers` (never mounted) yet surfaced as `StartupBlocked` in `SnapshotStatus` — Phase-16 mount policy unchanged | closed |
| T-29-02-06 | Tampering | CSRF on the new write surface | mitigate | `auth_cookie.go:124-133` session cookie `SameSite=Strict` + HttpOnly + Secure + `__Host-` prefix (CWE-352); the new routes inherit it (no separate cross-origin write path); `server_cors.go:7-22` CORS is opt-in (`CORSPermissive`, off by default) and emits `Access-Control-Allow-Origin: *` WITHOUT `Allow-Credentials` — a credentialed cross-origin write is browser-impossible | closed |
| T-29-02-07 | Denial of Service | hung MCP server stalls the board | mitigate | `serve_governance_write.go:37,189-194` post-write probe runs under `context.WithTimeout(ctx, mcpProbeTimeout=3s)`; `probe.go:18-21,43-77` ProbeServer is per-row, fail-soft (a dead server yields OK=false for its own row only) | closed |
| T-29-02-SC | Tampering | installs (plan 02) | mitigate | 29-02 is Go-only; no package installs (`tech-stack.added: []` in 29-02-SUMMARY) | closed |
| T-29-03-01 | Tampering | prompt injection via fetched skill body | mitigate | `installer.go:172` calls `ValidateForWrite(fm, body, blocklist, bodyCap, false)` (allowBlocklisted=false); `validator.go:58-69,82-106` NFKC-normalize-FIRST → casefold → literal-substring blocklist with matched byte position; body cap; staged to pending via `WriteInstallPending` (installer.go:176) BEFORE approval; `validator_fuzz_test.go` covers NFKC collapse + non-ASCII name | closed |
| T-29-03-02 | Tampering | pending skill body injected into LLM context | mitigate | `loader.go` Loader scans configured `Roots` only (active root in production wiring); `no_model_approve_test.go` `TestActiveLoaderExcludesPending` stages a pending skill, builds the Loader with active root only, asserts `List()`/`Get()` never return it | closed |
| T-29-03-03 | Elevation of Privilege | model self-approves/activates a skill | mitigate | `resume.go:48-57` `ResumeHandler.Resume` is the only path that calls `Writer.Activate` (line 51), reached only via the operator `/api/approvals` resolve; install endpoint stages to pending only; `no_model_approve_test.go` `TestNoModelFacingActivatePath` (type-resolved go/packages CallExpr scan over `internal/agent/tools`, receiver confirmed via `types.Info`) asserts COUNT==0 with a `scannedCalls>0` non-vacuity guard | closed |
| T-29-03-04 | Tampering | restore silently overwrites an active skill | mitigate | `serve_governance_write_skills.go:190-195` Restore calls `writer.ActiveExists(name)` and returns `ErrSkillActiveExists` BEFORE `Writer.Restore` (which does `os.RemoveAll`); `governance_write_skills_api.go:235-236` maps it to 409; `writer_activate.go:104-110` ActiveExists is a SanitizeName-guarded read-only stat | closed |
| T-29-03-05 | Repudiation | unaudited skill lifecycle action | mitigate | Writer paths append one `skill_audit` row inside `db.WithTx` (existing 0010 triggers); live-PG `TestInstallerAuditAppendOnly` (installer_integration_test.go:46-120) proves exactly one install row newest-first; pending absent from the active loader | closed |
| T-29-03-06 | Spoofing/Info Disclosure | silent external discovery escalation | mitigate | `installer.go:198-227` `Search` gated by `externalDiscoveryEnabled()` (`AURA_SKILLS_EXTERNAL_DISCOVERY`); off by default (line 220-226); disabled result carries the explicit toggle state | closed |
| T-29-03-07 | Denial of Service | hung/looping npx invocation | mitigate | `installer.go:141,206,279-289` runs via `exec.CommandContext` (ctx-bounded); fake runner in unit tests; container caps blast radius | closed |
| T-29-03-SC | Tampering | the `skills` npx CLI supply chain | mitigate | `installer.go:141` runs `npx skills add <source> -y` with NO `--ignore-scripts` (scripts permitted, container = boundary); D-09 rationale documented in `29-SPEC.md` (lines 88, 149: container-isolated + approval-gated + Writer-validated, "the control is NOT `--ignore-scripts`"); no new Go/npm deps added by the phase | closed |
| T-29-04-01 | Information Disclosure | secret value in the DOM | mitigate | `McpEnvEditForm.tsx:85-99` initializes secret rows to the redacted `${KEY}` placeholder token, never the value; plain `type="text"` input with NO eye-reveal; `McpEnvChip` DTO is key-only; Vitest (`McpEnvEditForm.test.tsx`) + Playwright DOM scan (`governance-write.spec.ts`) assert no value in DOM | closed |
| T-29-04-02 | Tampering | XSS via a backend string | mitigate | grep of `web/src/governance/` + `web/src/approvals/` for `dangerouslySetInnerHTML=` returns NO JSX usage (the only mention is a T-25-20 doc comment in InlineApprovalCard.tsx confirming non-use); all backend strings render as React-escaped text nodes | closed |
| T-29-04-03 | Elevation of Privilege | run/activate affordance on a pending skill | mitigate | `InlineApprovalCard.tsx:253-273` `SkillRiskStrip` renders RISKY badge + container note + resume token ABOVE the existing Answer/Decline/Cancel verbs — NO run/activate button (activation is the approval resume only); reuses the existing ApprovalBadge/queue (D-11); expired/consumed token → warning `TerminalChip` (lines 88-94) | closed |
| T-29-04-04 | Spoofing | silent external discovery | mitigate | `SkillInstallPanel.tsx:43` toggle defaults OFF; search input `disabled={!externalDiscovery}` (line 163) + off-note (170-173); catalog query `enabled: externalDiscovery && q!==''` (line 54), reflecting `AURA_SKILLS_EXTERNAL_DISCOVERY` | closed |
| T-29-04-05 | Tampering | accidental destructive remove | mitigate | `McpLifecycleCluster.tsx:180-265` single `RemoveDialog` (role=dialog, aria-modal) with action-specific labels (`removeConfirm`/`removeCancel` = Remove server / Keep server); destructive button NOT default-focused (`cancelRef.current?.focus()`, line 199); Escape-dismissable + focus-trapped; no kebab, no type-to-confirm | closed |
| T-29-04-SC | Tampering | npm installs (plan 04) | mitigate | no new npm deps; latest `web/package.json` change predates Phase 29 (Phase-27 commit); hand-rolled components over the locked token system | closed |
| T-29-05-01 | Information Disclosure | a secret leaking anywhere | mitigate | `governance_write_secret_scan_test.go` `TestGovernanceWriteSecretScan` drives a full MCP install + env-edit rotate + env-edit placeholder-preserve + skill install (secret-in-source) with the slog handler captured; asserts zero seeded secret VALUES in any response body OR log line; non-vacuous (fakes hold the real secret; preserves confirmed). Caught + fixed a real source-echo leak (`sanitizeSkillSource`) | closed |
| T-29-05-02 | Tampering | a regression making a ledger mutable | mitigate | live-PG append-only on BOTH ledgers: `TestMCPAuditAppendOnly` (mcp_audit) + `TestSkillAuditAppendOnly` (installer_integration_test.go:139-229, skill_audit) each assert UPDATE/DELETE/TRUNCATE → 42501 + rows survive | closed |
| T-29-05-03 | Elevation of Privilege | model-tool path gains a skill-activation edge | mitigate | `no_model_approve_test.go` `TestNoModelFacingActivatePath` — type-resolved go/packages scan over `internal/agent/tools`, COUNT==0 edges into `(*skills.Writer).Activate`/`(*skills.ResumeHandler).Resume`, `scannedCalls>0` negative control | closed |
| T-29-05-04 | Elevation of Privilege | an un-gated mutating route slips through | mitigate | `governance_write_auth_sweep_test.go` — `TestGovernanceWriteAuthSweep401` (12 mutating routes 401 unauthenticated) + `TestGovernanceWriteAuthSweep403` (all 13 routes 403 without governance.write, via the production `RequireCapability` mount) + grantee-reaches non-vacuity control | closed |
| T-29-05-05 | Tampering | stale dist / skipped integration tier | mitigate | `internal/webui/dist` rebuilt this phase (commit `cdf955d5`; assets include GovernanceWorkspace/McpBoard, Jun-21 timestamps); NO-SKIP-AS-GREEN: `envOrSkip` t.Fatals under `$CI` when the DSN is unset (audit_store_integration_test.go:34-45) | closed |
| T-29-05-SC | Tampering | installs (plan 05) | mitigate | no new module installs; `golang.org/x/tools v0.44.0` is a transitive→direct promotion (already in the graph, no go.sum change), test-only import for the callgraph scan | closed |
| T-29-CANON-SSRF | Info Disclosure / SSRF | MCP probe + skill-source fetch | mitigate / accept (residual) | (a) `probe.go:18-21,43-77` ProbeServer targets ONLY the looked-up `ManagedServer` from operator config — never a request-body URL/command; an HTTP/streamable endpoint is reported "configured" without a dial → no unguarded outbound reachable by an attacker. (b) `npx skills add <source>` reaches the network but `source` is operator-authored behind `RequireCapability(governance.write)` + container isolation + `exec.CommandContext` bound, and the echo is redacted by `sanitizeSkillSource` (governance_write_skills_redact.go). (c) project SSRF precedent `image_proxy.go` → `FetchImage` (hostname blocklist→DNS-pin→allowlist→size cap→no redirect) confirmed present. Residual (probe stdio-only / fetch operator-gated) recorded in the Accepted Risks Log | closed |
| T-29-CANON-TRAVERSAL | Tampering / Path Traversal | skill name/path on every Writer entry | mitigate | `validator.go:36-49` `SanitizeName` `^[a-z0-9-]{1,64}$` is the SINGLE chokepoint (D-30, "no second name validator"). Applied FIRST at every Writer entry that joins a name into a path: Activate (writer_activate.go:27), Archive (73), ActiveExists (105), Restore (129), Delete (162), SetAlways (238), DiscardPending (resume.go:67), snippet (307), snippet_usage (180), loader (241); create/update via `ValidateForWrite`→SanitizeName (writer.go:104); install via `ValidateNameAgainstDir`→SanitizeName (installer.go:160). No Writer path joins an unsanitized name | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-29-01 | T-29-01-04 | `aura.mcp_audit.reason` is operator-authored free-text, populated ONLY on the `trust` action (the operator's vetting note) and surfaced only behind `governance.read`/`governance.write`. By construction no env value or secret is routed into `reason`: the env-edit adapter path sets no `Reason`; install/enable/disable/remove set no `Reason`. The residual (an operator deliberately typing a secret into a trust reason) is an operator-authored, capability-gated, audit-only field — accepted, not mitigated. | davide marchetto (operator) | 2026-06-21 |
| AR-29-02 | T-29-CANON-SSRF | Residual SSRF surface on the MCP probe + skill-source fetch is accepted: (a) the probe is stdio/configured-servers-only — it never dials an attacker-controlled URL (an HTTP endpoint is reported as "configured" without a dial), so there is no unguarded outbound an attacker can reach; (b) the `npx skills add <source>` outbound is reachable ONLY by an operator holding `governance.write`, runs inside Aura's container (the isolation boundary, D-06/D-07), is `exec.CommandContext`-bounded, and the source echo is credential-redacted. The project SSRF guard precedent (`image_proxy.go`/web `FetchImage`) governs the one untrusted-URL fetch surface and is unchanged by this phase. No new unguarded attacker-reachable outbound request is introduced. | davide marchetto (operator) | 2026-06-21 |

*Accepted risks do not resurface in future audit runs.*

---

## Unregistered Flags

None. No `## Threat Flags` section appears in any of the five 29-0x-SUMMARY.md files; no new attack surface emerged during implementation without a threat mapping. (The three executor deviations — `sanitizeSkillSource` source-echo redaction, the cron migration-bound fix, and the e2e assertion scoping — are all in-scope corrections that strengthen, rather than expand, the declared threat surface.)

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-21 | 35 | 35 | 0 | gsd-security-auditor (Opus 4.8) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log (AR-29-01, AR-29-02)
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-21
