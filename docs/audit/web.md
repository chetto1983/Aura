# Audit: internal/web

**Verdict:** needs-work — one logic bug (timeout misclassified as http_error), one dead branch, one dead parameter, and one medium-severity SSRF concern.

**Counts:** critical 0 / high 1 / medium 2 / low 1

## Findings

---

### [HIGH][BUG] Fetch deadline fires as `http_error`, not `timeout`

**Location:** `internal/web/fetcher.go:99,102,226-233` + `internal/web/fetcher.go:215-221`

**Confidence:** high

**Detail:**
`classifyTransportErr` converts every non-SSRF transport error — including a `context.DeadlineExceeded` wrapping from `http.Client.Do` — into the opaque sentinel `errRetryable`, discarding the original error type. When the per-fetch timeout (set at fetcher.go:72) fires, the sequence is:

1. `http.Client.Do` returns a `*url.Error` wrapping `context.DeadlineExceeded` (verified: `errors.Is(err, context.DeadlineExceeded) == true`, `ne.Timeout() == true`).
2. `classifyTransportErr` maps it to `errRetryable` (not `*internalError`, so falls to the generic branch).
3. `fetchBody` checks `errors.Is(err, errRetryable) && attempt == 0 && ctx.Err() == nil`. When the timeout has fired, `ctx.Err() != nil`, so the condition is false and the code falls through to `classifyFetchErr(errRetryable)`.
4. `classifyFetchErr` tests `errors.Is(errRetryable, context.DeadlineExceeded)` → false. `isNetTimeout(errRetryable)` → false. Returns `CodeHTTPError`.

Result: a fetch that times out is surfaced to the model as `{"error":"http_error","message":"fetch failed"}` instead of `{"error":"timeout","message":"fetch timed out"}`. The `CodeTimeout` branch in `classifyFetchErr` is unreachable for timeout errors on the fetch path (context.DeadlineExceeded never arrives there directly — it is always laundered through errRetryable first). The SearXNG path does not share this bug because `searxGet` checks `ctx.Err()` directly before returning `errSearxUnreachable`.

**Suggested fix:** In `classifyTransportErr`, preserve deadline/net-timeout errors rather than erasing them into `errRetryable`:

```go
func classifyTransportErr(err error) error {
    var ie *internalError
    if errors.As(err, &ie) {
        return ie
    }
    // Preserve deadline / net-timeout so classifyFetchErr can produce CodeTimeout.
    if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || isNetTimeout(err) {
        return err  // classifyFetchErr will match it
    }
    return errRetryable
}
```

Alternatively, check `ctx.Err()` in `classifyFetchErr` before the generic fallthrough, or check it directly after `doHops` returns.

---

### [MEDIUM][DEAD-CODE] `metadataV6Pfx` case in `classify` is unreachable

**Location:** `internal/web/ssrf.go:55-57`

**Confidence:** high

**Detail:**
`fd00:ec2::/32` is a sub-prefix of the ULA range `fc00::/7`. `netip.Addr.IsPrivate()` in Go's standard library returns true for all `fc00::/7` addresses, which subsumes `fd00:ec2::/32`. The `ip.IsPrivate()` switch case (ssrf.go:44) fires first and returns `"private", true` for any address in `fd00:ec2::/32`, so the `metadataV6Pfx.Contains(ip)` case (ssrf.go:55) is dead code. Verified empirically: `netip.MustParseAddr("fd00:ec2::1").IsPrivate()` returns `true`. The test file acknowledges this at ssrf_test.go:39-42 ("known dead branch flagged at the Gate-3 mutation review"), but the dead code remains in production.

The `metadataV6Pfx` package-level variable is initialised but only referenced in this unreachable branch, so both the variable and the branch are dead.

**Suggested fix:** Remove the `case metadataV6Pfx.Contains(ip)` branch and the `metadataV6Pfx` variable. The security property is already covered by `ip.IsPrivate()`. Update the comment to note that `fd00:ec2::` is blocked as ULA via `IsPrivate`, not a dedicated prefix.

---

### [MEDIUM][DEAD-CODE] `newGuard` third parameter `_ any` is always `nil`

**Location:** `internal/web/ssrf.go:75`

**Confidence:** high

**Detail:**
`newGuard(res resolver, pin *dnsPin, _ any)` accepts a third parameter that is discarded (`_`) and is passed as `nil` at every call site in the repo (client.go:28, fetcher_test.go:35, ssrf_test.go:104/118/153/184/193/218, transport_test.go:52/71/90/105). The body of `newGuard` only uses `res` and `pin`. The third slot is dead — it adds noise to every call site and suggests an incomplete extension that was never wired.

**Suggested fix:** Remove the third parameter from `newGuard` and update all call sites to drop the `nil` argument.

---

### [LOW][BUG] `validHostname` accepts `@`, `#`, and newline in domain strings

**Location:** `internal/web/searxng.go:169-175`

**Confidence:** medium

**Detail:**
`validHostname` only rejects characters in `"/:? "`. It accepts:
- `user@evil.com` → produces `site:user@evil.com` in the SearXNG query (RFC 3986 user-info abuse, semantically nonsensical to SearXNG but unexpected)
- `evil.com#fragment` → produces `site:evil.com#fragment`
- `evil.com\nX-Header: injected` → newline injection in the query value

`url.Values.Encode()` URL-encodes the newline (`%0A`), so HTTP header injection in the GET request is not possible. The site-filter clause is sent as a query parameter value, not a raw header, so the practical impact is limited to a malformed SearXNG query rather than a security bypass. The model supplies `Domains`, and the SearXNG backend (not a model-accessible target) receives the garbled `site:` clause. No SSRF vector is opened since this code path does not resolve or fetch the domain. Severity is low.

**Suggested fix:** Add `@`, `#`, `\r`, `\n` to the `ContainsAny` blocklist, or replace the blocklist approach with a strict `net.ParseIP` / `idna.Lookup` acceptance check:

```go
func validHostname(d string) string {
    d = strings.TrimSpace(strings.ToLower(d))
    if d == "" || strings.ContainsAny(d, "/:? @#\r\n") {
        return ""
    }
    return d
}
```
