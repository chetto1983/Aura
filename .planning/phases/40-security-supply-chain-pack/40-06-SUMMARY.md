---
phase: 40-security-supply-chain-pack
plan: 06
status: complete
completed: 2026-07-30
requirements: [SEC-01]
---

# Plan 40-06 Summary — configured-secret at-rest redaction

## Outcome

The inbound half of F-021 / SEC-01 is closed. A single exact-value redactor
harvests secret-shaped values from Aura's effective process environment through
the canonical `secret.IsSecretEnvVar` predicate. Values shorter than twelve
bytes are excluded to avoid accidental substring corruption.

The snapshot initializes from the inherited environment and is atomically
refreshed by the config loader after `.env` is loaded. Production therefore
captures the effective boot configuration, while rotations still require the
normal process restart.

The redactor is applied before:

- conversation inline persistence;
- conversation spill selection and `.content` writing;
- the no-spill transaction guard;
- tool-result preview sizing and `.result` writing, including the
  reserved-tail variant.

Redacting before byte counts and truncation keeps `Bytes`, preview offsets, and
`read_tool_output` offsets consistent with the actual persisted sidecar.
Unknown values—including secrets discovered by the agent—are not pattern
redacted on this inbound path.

## Verification

- full tests: `internal/secret`, `internal/config`,
  `internal/agent/tools`, `internal/conversations` — pass;
- whole-tree build and affected-package vet — pass;
- WSL race tests for exact redaction and both persistence seams — pass;
- real Postgres `db_integration` round-trip
  `TestAppendTurnRedactionAtEveryAtRestSurface` — pass, proving absence of the
  configured value in the row, `.content`, and `.result`, while preserving an
  agent-discovered control value;
- sidecar permissions remain `0600` on POSIX.

## Accepted tradeoffs

Exact substring replacement can redact a coincidental occurrence of a long
configured value in otherwise legitimate content. The twelve-byte floor keeps
that collision probability negligible and prioritizes zero false negatives for
Aura's own credentials at rest.
