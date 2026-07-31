# Aura current audit state

Date: 2026-07-31
Policy: PRD Amendment #106.5

This directory contains current unresolved state only. Historical audits,
closed findings, plans, handoffs, experiments, and scores were retired from the
working tree by explicit operator direction. Their last complete tree is
recoverable from Git commit
`fb8c40c0f53fa958665845d7e7217d0aa4d9796a`.

## Current verdict

- Audit disclosure gate: **PASS** when all rows below are present and valid.
- Release readiness: **NO-GO** while any row remains.
- Current unresolved: **5** — all are external constraints.
- No current implementation score is published. The archived 10.0 closure
  score and 9.8 direct-tool score are historical evidence, not release
  certification.

## Unresolved issues

| ID | State | Current evidence | Closure condition | Owner |
|---|---|---|---|---|
| EXT-001 | external_blocked | Calendar MCP has no authorized provider account; direct calls fail closed | Connect a dedicated test account and complete reversible create, read, update, and delete | Operator |
| EXT-002 | external_blocked | Email MCP has no authorized sender account or designated recipient | Connect a test account and verify one authorized delivery receipt | Operator |
| EXT-003 | external_blocked | WhatsApp bridge is healthy but remains `waiting_qr` and unpaired | Pair an authorized test device and complete message, reaction, file, audio, and media round trips | Operator |
| EXT-004 | external_blocked | GHCR version `845339375`, digest `sha256:764b4b3e58ebb1e627c54b6c10a0e9889b43caa53cb06728d12803ca53415628`, is untagged but GitHub refuses API deletion after more than 5,000 downloads; all known tags are absent | GitHub Support deletes the protected public package version and a packages-authorized query verifies zero versions | GitHub Support |
| EXT-005 | external_blocked | Native `send_file` reached the correct contextual guard but has no authorized channel delivery receipt | Deliver one reversible fixture through an authorized channel and verify the receipt and cleanup | Operator |

## Machine check

```bash
PYTHONPATH=scripts python3 scripts/audit_closure_gate.py
```

The disclosure gate passing does not make the release publishable. Its report
must emit `release_ready:false` until this table is empty.
