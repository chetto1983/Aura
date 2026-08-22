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
| EXT-001 | open | Account IS authorized (`davide`, Google, enabled). Blocker is now INTERNAL: the running daemon mounted `server=calendar tools=14` at 12:50 while the MCP server now exposes one multiplexed `calendar` tool, so every per-action call returns `Unknown tool: '<action>'`. Two real agent runs (2026-08-22) ended in `RUN_ERROR` with zero successful calls | Reversible create, read, update, and delete completed through the live agent | Operator |
| EXT-002 | open | Account IS authorized and READING works — `Retrieved 30 emails from Google account davide` (2026-08-22 12:36). The send leg is unproven and shares EXT-001's broken tool surface | One authorized delivery receipt verified | Operator |
| EXT-003 | open | Device IS paired and the bridge carries live traffic; the tool plane is proven (12 `whatsapp__*` calls, `RUN_FINISHED`, 2026-08-22). Round trips are WITHHELD by the ToolGateway: a send returns `gateway_approval_required` pending operator approval | Message, reaction, file, audio, and media round trips completed after approval | Operator |
| EXT-004 | external_blocked | GHCR version `845339375`, digest `sha256:764b4b3e58ebb1e627c54b6c10a0e9889b43caa53cb06728d12803ca53415628`, is untagged but GitHub refuses API deletion after more than 5,000 downloads; all known tags are absent | GitHub Support deletes the protected public package version and a packages-authorized query verifies zero versions | GitHub Support |
| EXT-005 | open | Native `send_file` reached the correct contextual guard but has no authorized channel delivery receipt; still unproven at the 2026-08-22 re-measurement | Deliver one reversible fixture through an authorized channel and verify the receipt and cleanup | Operator |

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
