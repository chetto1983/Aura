# Security Surface Audit: Aura — 2026-05-17

Read-only audit by subagent. Looks for SQL injection, command injection,
path traversal, auth bypass, secret leakage, untrusted-input parsing
without size caps, TLS verification disabled, CORS/CSRF gaps, Telegram
sender validation.

## Status: 0 confirmed vulnerabilities

All examined attack surfaces are properly defended.

### 1. SQL injection — SAFE
- `internal/dbrecovery/recovery.go:217,230` use `Sprintf` but only with
  schema metadata (`PRAGMA table_info`), never user input. Identifier
  escaping via `quoteIdent`/`quoteList`/`placeholders` helpers.
- All data values use `?` placeholders + `Args...` everywhere else.

### 2. Command injection — SAFE
- `internal/mcp/client.go:261` spawns servers from `mcp.json` (operator
  config, not LLM input).
- `internal/skills/admin.go:77` runs `npx` with hardcoded binary +
  capability-gated skill name.
- `internal/sandbox/` Python sandbox runner uses fixed interpreter path.
- `execute_shell` / `execute_code` tools accept LLM commands BY DESIGN
  (documented in CLAUDE.md, gated by capability checks) — not a finding.

### 3. Path traversal — SAFE
- `internal/api/files.go:120-135` rejects absolute paths + `..`,
  resolves both ends to canonical form, prefix-checks with separator
  guard (`!strings.HasPrefix(resolved+"/", dirAbs+"/")`).
- Wiki + source store use slug/auto-generated names, not raw HTTP input.

### 4. Auth bypass — SAFE
- Bearer token middleware uses `crypto/subtle.ConstantTimeCompare`.
- Every API route wrapped via `NewRouter()` — no bypass shortcuts found.
- `internal/telegram/access.go:221 allowlistIdentitySource()` called
  BEFORE every command handler; state mutation gated on allowlist check.
- `internal/identity/store_auth.go Authorize` is the single decision
  path; called from every sensitive operation.

### 5. Secret leakage in logs — SAFE
- Zero log statements found that log raw `args`, request bodies, or
  secret values (MISTRAL_API_KEY, TELEGRAM_TOKEN, OPENROUTER_API_KEY,
  AURA_CHAT_TOKEN, OPENAI_API_KEY).
- `internal/skills/admin.go:299 NormalizeRuntimeEnv()` redacts
  TELEGRAM_TOKEN + MISTRAL_API_KEY before subprocess spawn.

### 6. Untrusted input size caps — SAFE
| Input | Site | Cap |
|---|---|---|
| Web fetch | direct_fetch.go | 8 MiB |
| SearXNG response | searxng.go | configured |
| Qdrant response | qdrant/client.go | 4 MiB |
| Embedding batch | embed_batch.go | 16 MiB |
| OCR response | ocr/client.go | 256 MiB |
| PDF upload | api/upload.go | 100 MiB default |
| Dashboard file read | api/files.go | 1 MiB |
| MCP tool output | mcp/client.go | 64 KiB |

All external inputs have explicit `io.LimitReader` caps before
deserialization.

### 7. TLS verification — SAFE
- Zero matches for `InsecureSkipVerify: true` in production code.
- No custom Transport that disables cert validation.

### 8. Dashboard CORS / CSRF — SAFE
- Bearer-token-only auth (Authorization header, never cookies).
- No CORS headers that leak credentials cross-origin.
- All state-mutating endpoints are POST/PUT/DELETE.
- CSRF protection is inherent to the bearer-token design.

### 9. Telegram allowlist enforcement — SAFE
- Sender check happens at the inbound entry point before ANY state
  change or tool dispatch.

## Operational recommendations (not findings)
- Periodically review `authz_decisions` table for suspicious patterns.
- Default 30-day bearer token TTL — consider shortening for high-risk deploys.
- 256 MiB OCR cap is intentional but watch for DoS-shape upload patterns.

---

Audit produced 2026-05-17 after the cleanup sweep. The codebase
demonstrates mature security practices.
