# Aura current audit state

Date: 2026-08-25 (rows re-measured against the running deployment; register created 2026-07-31)
Policy: PRD Amendment #106.5

This directory contains current unresolved state only. Historical audits,
closed findings, plans, handoffs, experiments, and scores were retired from the
working tree by explicit operator direction. Their last complete tree is
recoverable from Git commit
`fb8c40c0f53fa958665845d7e7217d0aa4d9796a`.

## Current verdict

- Audit disclosure gate: **PASS** when all rows below are present and valid.
- Release readiness: **NO-GO** while any row remains.
- Current unresolved: **4** — **one** is an external constraint (EXT-004). The other three
  were re-measured on 2026-08-22 and their original evidence no longer held: the accounts are
  connected and the device is paired. The internal defect that blocked EXT-001/EXT-002 was fixed
  and proven on 2026-08-23; what blocks all three now is an authorized WRITE — a reversible CRUD,
  a send receipt, or an approval the gateway withholds by design (EXT-003).
- EXT-005 left the current register on 2026-08-25. Its authenticated delivery and receipt were
  already observed on 2026-08-22; the same persistent deployment now has an empty
  `/var/lib/aura/runs` root, so both the TTL-managed staging subtree and the two recorded legacy
  `aura-sendfile-*` directories are absent. This proves cleanup without claiming which boot or
  periodic sweep removed each entry.
- No current implementation score is published. The archived 10.0 closure
  score and 9.8 direct-tool score are historical evidence, not release
  certification.

## Unresolved issues

| ID | State | Current evidence | Closure condition | Owner |
|---|---|---|---|---|
| EXT-001 | open | The internal blocker is RESOLVED (2026-08-23): the curated surface landed (`2edbc3910`, `b01413620`, `8cc2aeed2`) and the daemon re-mounted with `up -d` — `mcp mounted server=calendar tools=1`. A real agent turn returned `RUN_FINISHED` with three successful `calendar__calendar` calls, zero `Unknown tool`, listing both accounts and tomorrow's single event. What remains is this row's own criterion: the READ leg is proven, the reversible write leg needs the operator's explicit go-ahead because it mutates their real calendar | Daemon re-mounted against the curated surface, then reversible create, read, update, and delete completed through the live agent | Operator |
| EXT-002 | open | Account IS authorized and READING works — `Retrieved 30 emails from Google account davide` (2026-08-22 12:36). It no longer shares a broken tool surface: EXT-001's re-mount is done and proven (2026-08-23). The send leg is still unproven, and sending is an outward-facing action no session may take unprompted | One authorized delivery receipt verified | Operator |
| EXT-003 | open | Device IS paired and the bridge carries live traffic; the tool plane is proven (12 `whatsapp__*` calls, `RUN_FINISHED`, 2026-08-22). A send is WITHHELD by the ToolGateway and the HITL chain is proven END-TO-END: the withheld result was relayed, and the withheld result was relayed and a wire-valid `approval` pause was raised (token `01a02b8a-fb00-…`, priority 80, `resume_context.args_sha256` matching the withheld call, question naming only the argument KEYS). **That pause is GONE as of 2026-08-23** and it was never answered: `GET /api/approvals` returns `[]`, and `aura.paused_states` holds no row for the token — the newest row in the whole table is 2026-08-16. Answered pauses persist with `resumed_at` set, so this one was destroyed, not resolved; the only removal path is the FK cascade from a hard-deleted conversation. The HITL chain remains proven, but this row now needs a FRESH withheld send to approve — the old token is unrecoverable | Operator accepts the pending approval, then message, reaction, file, audio, and media round trips | Operator |
| EXT-004 | external_blocked | GHCR version `845339375`, digest `sha256:764b4b3e58ebb1e627c54b6c10a0e9889b43caa53cb06728d12803ca53415628`, is untagged but GitHub refuses API deletion after more than 5,000 downloads; all known tags are absent | GitHub Support deletes the protected public package version and a packages-authorized query verifies zero versions | GitHub Support |

## Machine check

```bash
PYTHONPATH=scripts python3 scripts/audit_closure_gate.py
```

The disclosure gate passing does not make the release publishable. Its report
must emit `release_ready:false` until this table is empty.

Closing a row is a TWO-PART change, not a documentation edit: `release_ready` is
literally `not rows` (`scripts/audit_closure_gate.py:132`) and
`REQUIRED_CURRENT_IDS` (`:25`) pins EXT-001..EXT-004, so deleting a row here
without deleting its ID there fails the gate with `missing current issues`.
`external_blocked` rows count toward `open_total` exactly like `open` ones —
there is no waiver state, so recording WHY a row stays open can never turn the
gate green.
