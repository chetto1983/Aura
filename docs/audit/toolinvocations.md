# Audit: internal/toolinvocations

**Verdict:** needs-work — two medium-severity regex gaps in the security-critical RedactForLedger seam; one low-severity not-wired method; no crashes or races.

**Counts:** critical 0 / high 0 / medium 2 / low 2

## Findings

---

### [MEDIUM][BUG] `bearer_token` regex misses `+` and `/` — partial credential leak for non-base64url tokens

**Location:** `internal/toolinvocations/redact.go:61`
**Confidence:** high

**Detail:**
The `bearer_token` pattern `(?i)bearer\s+[A-Za-z0-9._\-]+` does not include `+` or `/` in its character class. A Base64-standard encoded token (as opposed to base64url) that contains `+` or `/` will be partially redacted: only the segment before the first `+` or `/` is replaced with `[REDACTED]`, and the remainder leaks.

Demonstrated with `"using bearer abc+def/ghi"` → `"using [REDACTED]+def/ghi"`.

This matters because:
1. `bearer_token` is the fallback pattern for Bearer credentials that appear outside a full `Authorization:` header (e.g. in a log line or error string copied into a result preview). In that context the `authorization_header` pattern does not fire, so `bearer_token` is the sole guard.
2. While OAuth2/OIDC JWTs use base64url (safe), some legacy APIs and Basic-auth-derived credentials use standard base64 with `+` and `/`.

Note: when the token appears inside a full `Authorization: Bearer ...` header, the `authorization_header` pattern (`[^"\r\n]*`) catches the full value including `+` and `/`, so the gap is limited to the standalone-bearer context.

**Suggested fix:**
Extend the charset to `[A-Za-z0-9._\-+/=]+` (adds `+`, `/`, and `=` padding):

```go
{"bearer_token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-+/=]+`)},
```

---

### [MEDIUM][BUG] `aws_akid` pattern covers only AKIA-prefix keys; temporary ASIA credentials are not caught

**Location:** `internal/toolinvocations/redact.go:65`
**Confidence:** high

**Detail:**
The `aws_akid` pattern `AKIA[0-9A-Z]{16}` matches only permanent IAM Access Key IDs (prefix `AKIA`). AWS temporary session credentials issued by STS use the `ASIA` prefix (also 20 characters: `ASIA` + 16 alphanumeric). These are *more* sensitive than long-term keys (they carry a SessionToken alongside), yet the pattern leaves them unredacted.

Additionally, other AWS key prefixes (`AROA` = role, `AIDA` = user, `ANPA`/`ANVA`/`AIAA` = managed policy) are also not covered.

For ASIA keys in a variable-assignment context (`AWS_ACCESS_KEY_ID=ASIA...`), the `inline_credential` pattern also does not fire because `AWS_ACCESS_KEY_ID` contains none of the keywords (`password`, `api_key`, `token`, `secret`). The leak is complete in that case.

**Demonstrated:**
```
AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE  →  unchanged (no pattern matches)
```

**Suggested fix:**
Replace the AKIA-only pattern with a broader AWS key prefix regex:

```go
{"aws_key", regexp.MustCompile(`(AKIA|ASIA|AROA|AIDA|ANPA|ANVA|AIAA)[0-9A-Z]{16}`)},
```

---

### [LOW][NOT-WIRED] `Store.ListByConversation` is not reachable from any production code path

**Location:** `internal/toolinvocations/store.go:73-87`
**Confidence:** high

**Detail:**
`ListByConversation` is a public method on `*Store`, but the production-facing interface that consumers depend on (`runner.ToolInvocationStore`, defined in `internal/runner/interfaces.go:75-77`) declares only `Insert`. No non-test production code calls `ListByConversation`. All references outside the defining file are in test files:

- `internal/toolinvocations/store_integration_test.go` (lines 158, 232)
- `internal/eval/skills_snippet_reuse_cot_eval_test.go` (line 418)

This means the read path of the ledger is exercised only in tests. If a future feature requires reading the ledger in production (e.g. a budget gate, a cost dashboard, or an AG-UI audit panel), the caller would need to widen the interface or accept the concrete `*Store`. As-is, the method is effectively dead in production.

This is not necessarily wrong (eval harnesses legitimately need to read the ledger), but the method is invisible to production consumers through the declared interface and has no production caller today.

**Suggested fix:**
If the method is intended only for tests/evals, document it:
```go
// ListByConversation returns all tool invocation events for one conversation.
// Not part of the production ToolInvocationStore interface; used by integration
// tests and eval harnesses to assert ledger state.
```
If a production feature needs it (e.g. a call-budget gate), add it to the interface at that point.

---

### [LOW][BUG] `authorization_header` regex: value truncated at embedded escaped-quote byte, leaving tail unredacted

**Location:** `internal/toolinvocations/redact.go:59`
**Confidence:** medium

**Detail:**
The `authorization_header` pattern uses `[^"\r\n]*` to consume the header value. This stops at the first bare `"` byte (0x22). In practice, the `Arguments` field contains the raw JSON string from the LLM wire (`call.Function.Arguments`), where literal double-quotes inside a JSON string value are encoded as `\"` (two bytes: 0x5C 0x22). The regex stops at 0x22, so the match ends just before the escaped-quote delimiter.

For the common JSON shell-command case:
```
{"command":"curl -H \"Authorization: Bearer sk-abc\" https://x"}
```
The regex sees `Authorization: Bearer sk-abc\` (backslash consumed, `"` stops match). The credential `sk-abc` IS redacted. The issue is the `\"` suffix itself: the `\` is consumed into the match (redacted) but the `"` is left behind as a stray character, producing `[REDACTED]"https://x"`. This is cosmetic and does not expose the secret.

The genuine security gap arises only if a credential value contains a literal bare `"` byte (0x22) in a non-JSON context: e.g. `Authorization: Bearer sk-abc"leaked`. In that case `sk-abc` is redacted but `"leaked` survives. However:
- HTTP Bearer tokens (RFC 6750) are base64url-encoded and cannot contain `"`.
- OpenAI/OpenRouter `sk-*` keys are alphanumeric and cannot contain `"`.

Risk in practice is very low. The gap is real but requires a pathological credential format.

**Suggested fix:**
Replace `[^"\r\n]*` with `\S*` (run of non-whitespace) to consume through quoted delimiters:
```go
{"authorization_header", regexp.MustCompile(`(?i)authorization\s*[:=]\s*\S+`)},
```
Or, for a more surgical fix, add `"` to the allowed charset:
```go
{"authorization_header", regexp.MustCompile(`(?i)authorization\s*[:=]\s*[^\r\n]+`)},
```
Note: `[^\r\n]+` also removes the quoted-value partial-match cosmetic issue.
