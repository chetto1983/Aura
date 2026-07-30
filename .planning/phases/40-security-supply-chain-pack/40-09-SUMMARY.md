---
phase: 40-security-supply-chain-pack
plan: 09
status: complete
completed: 2026-07-30
requirements: [SEC-01]
commits: []
---

# Plan 40-09 Summary — encrypted reasoning traces

## Outcome

SEC-01 is closed across both halves. The earlier inbound work exact-redacts
Aura-configured secret values before every at-rest turn, spill, tool-result, and
sidecar write. This plan closes the reasoning-trace half:

- trace values use the canonical `secret.IsSecretEnvVar` predicate, including
  `AURA_DB_URL`, rather than a local four-marker denylist;
- credential-bearing URLs typed into free text receive one outbound
  `redact.String` pass;
- strict profiles reject `AURA_REASONING_TRACE=full` unless
  `AURA_TRACE_FULL_ACK=1`;
- full records use a new fail-closed encrypted sink: AES-256-GCM, a fresh random
  nonce for every record, length-delimited nonce-prefixed framing, and a
  32-byte key derived from a 64-hex operator key through HKDF-SHA-256 with the
  domain label `aura-reasoning-trace-sink-v1`;
- an absent or malformed encryption key drops the full record and warns once.
  No plaintext fallback exists.

Summary-mode JSONL remains compatible. The trace-mode registry row is a string,
not a restrictive enum, because the runtime already supports the legacy
`1/true/yes/on` spellings in addition to `summary/full`.

## Verification

- TDD red state observed for the missing sink, plaintext full mode, missing-key
  fallback, and missing production gate.
- `go test` and `go test -race` passed for `internal/tracesink`,
  `internal/reasoningtrace`, and `internal/config`.
- New sink statement coverage: 85.7%.
- `golangci-lint` over all three packages: 0 issues.
- CodeQL CLI 2.26.2 with `codeql/go-queries` 1.6.7 and
  `go-security-and-quality.qls`: 0 results across the targeted extraction (151
  Go files, including the three changed packages and their dependencies).
- Tests prove fresh nonces, decrypt round-trip, tamper/truncation rejection,
  framed multi-record append, no plaintext in the full-trace file, missing-key
  refusal, escaped configured-secret redaction, literal DSN redaction, and the
  strict/lenient profile matrix.

## Operational contract

An operator enabling full trace must supply all three:

```text
AURA_REASONING_TRACE=full
AURA_TRACE_FULL_ACK=1
AURA_TRACE_ENCRYPT_KEY=<64 hex characters>
```

The acknowledgement is mandatory at strict-profile validation. Encryption is
mandatory in every profile whenever full mode writes. Existing plaintext full
trace files are not migrated automatically and should be removed according to
the configured trace-retention policy.
