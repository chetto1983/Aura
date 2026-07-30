---
phase: 40-security-supply-chain-pack
plan: 08
status: complete
completed: 2026-07-30
requirements: [SEC-09]
commits: []
---

# Plan 40-08 Summary — peppered reset-token lookup

## Outcome

SEC-09 is closed. Reset-token database lookup keys now use HMAC-SHA-256 with a
32-byte pepper derived from `AURA_AUTHULA_SECRET` through HKDF-SHA-256 and the
domain label `aura-reset-token-pepper-v1`. Invalid or missing 64-hex input fails
closed; the raw Authula secret is never used directly as the HMAC key.

The same pepper is threaded through:

- the online challenge mint in `recoveryStoreAdapter`;
- `PasswordResetService.Complete`;
- the host-only `aura identity recover` break-glass mint.

The CLI validates the secret before opening the database or minting. The
throwaway challenge code remains Argon2id-hashed. A missing token pepper also
prevents service wiring and break-glass minting.

## Verification

- `go test ./internal/agui -count=1` — pass.
- focused `cmd/aura` recovery and password-reset tests — pass.
- `go build ./...` and affected-package `go vet` — pass.
- WSL race detector over recovery/password-reset tests — pass.
- live Postgres `TestMintBreakGlassTokenRoundTrip` with `db_integration` and
  `-race` — pass; the plaintext token resolved only through the same peppered
  lookup key.
- CodeQL CLI 2.26.2, `codeql/go-queries` 1.6.7,
  `go-security-and-quality.qls` — zero results on
  `internal/agui/recovery_hash.go`; the previous
  `go/weak-sensitive-data-hashing` result is cleared.

The CodeQL run also produced a separate 26-result repository baseline outside
`recovery_hash.go`. Those results are not treated as part of SEC-09 and are
tracked for the broader audit convergence rather than hidden by this closure.

## Deployment note

Deploying the keyed lookup invalidates reset tokens minted by an older build.
There is no backfill because plaintext tokens are intentionally unavailable;
the existing ten-minute TTL bounds the compatibility window. Operators should
avoid deploying during an active password-reset handoff or ask the user to
restart the reset flow.

Break-glass coherence is operational: the CLI and server must receive the same
`AURA_AUTHULA_SECRET`. Equality cannot be checked across processes without
exposing secret-derived material, so both paths fail closed on absent or
malformed input.
