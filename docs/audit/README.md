# Aura current audit state

Date: 2026-08-23 (rows re-measured against the running deployment; register created 2026-07-31)
Policy: PRD Amendment #106.5

This directory contains current unresolved state only. Historical audits,
closed findings, plans, handoffs, experiments, and scores were retired from the
working tree by explicit operator direction. Their last complete tree is
recoverable from Git commit
`fb8c40c0f53fa958665845d7e7217d0aa4d9796a`.

## Current verdict

- Audit disclosure gate: **PASS** when all rows below are present and valid.
- Release readiness: **NO-GO** while any row remains.
- Current unresolved: **5** — **one** is an external constraint (EXT-004). The other four
  were re-measured on 2026-08-22 and their original evidence no longer held: the accounts are
  connected and the device is paired. What blocks them now is an internal defect (EXT-001,
  EXT-002) and an approval the gateway withholds by design (EXT-003, EXT-005).
- No current implementation score is published. The archived 10.0 closure
  score and 9.8 direct-tool score are historical evidence, not release
  certification.

## Unresolved issues

| ID | State | Current evidence | Closure condition | Owner |
|---|---|---|---|---|
| EXT-001 | open | Account IS authorized (`davide`, Google, enabled). Blocker is now INTERNAL and identified: the daemon mounted `server=calendar tools=14` at 12:50, then the PIM MCP was swapped for the curated fork that advertises ONE multiplexed `calendar` tool. The daemon never re-mounted, so it still offers 14 stale per-action names and every call returns `Unknown tool: '<action>'`. Two real agent runs (2026-08-22) ended in `RUN_ERROR` with zero successful calls | Daemon re-mounted against the curated surface, then reversible create, read, update, and delete completed through the live agent | Operator |
| EXT-002 | open | Account IS authorized and READING works — `Retrieved 30 emails from Google account davide` (2026-08-22 12:36). The send leg is unproven and shares EXT-001's broken tool surface | One authorized delivery receipt verified | Operator |
| EXT-003 | open | Device IS paired and the bridge carries live traffic; the tool plane is proven (12 `whatsapp__*` calls, `RUN_FINISHED`, 2026-08-22). Round trips are WITHHELD by the ToolGateway: a send returns `gateway_approval_required` pending operator approval | Message, reaction, file, audio, and media round trips completed after approval | Operator |
| EXT-004 | external_blocked | GHCR version `845339375`, digest `sha256:764b4b3e58ebb1e627c54b6c10a0e9889b43caa53cb06728d12803ca53415628`, is untagged but GitHub refuses API deletion after more than 5,000 downloads; all known tags are absent | GitHub Support deletes the protected public package version and a packages-authorized query verifies zero versions | GitHub Support |
| EXT-005 | open | Two of three legs now PROVEN live (2026-08-22): delivery through the authenticated cockpit channel, and a receipt — the `aura.artifact` frame carries `asset_id` 736e1d66-bc28-44b0-be7b-22359dd3095d, and `aura.assets` holds it `accepted`, 29 bytes, in the per-identity bucket with a content hash. The cleanup leg is unobserved: the stage dir lives under `$AURA_RUN_DIR/tmp/`, which `conversations.ScanOrphans` sweeps at a 24h TTL, so removal falls outside the measuring session | Deliver one reversible fixture through an authorized channel and verify the receipt and cleanup | Operator |

### Residue found while re-measuring (not an unresolved row)

`/var/lib/aura/runs/aura-sendfile-3383401088/` and `aura-sendfile-830219956/`
have held a 13 KB and a 6.6 KB `dashboard.html` since 2026-07-28. The
`aura-sendfile-` prefix appears nowhere in the Go tree any more — the current
stager is `aura-boxstage-*` under `$AURA_RUN_DIR/tmp/` — so these two sit
OUTSIDE the swept `tmp/` subtree and nothing will ever remove them. Legacy
residue of a removed code path holding user content, not a live leak.

## Machine check

```bash
PYTHONPATH=scripts python3 scripts/audit_closure_gate.py
```

The disclosure gate passing does not make the release publishable. Its report
must emit `release_ready:false` until this table is empty.

Closing a row is a TWO-PART change, not a documentation edit: `release_ready` is
literally `not rows` (`scripts/audit_closure_gate.py:132`) and
`REQUIRED_CURRENT_IDS` (`:25`) pins EXT-001..EXT-005, so deleting a row here
without deleting its ID there fails the gate with `missing current issues`.
`external_blocked` rows count toward `open_total` exactly like `open` ones —
there is no waiver state, so recording WHY a row stays open can never turn the
gate green.
