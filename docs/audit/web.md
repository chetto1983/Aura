# Audit: internal/web

**Verdict:** needs-work — two unreachable return statements mask retry logic bugs; one model-visible reason string outside the stable D-38 enum; one map that grows without bound.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][DEAD-CODE] Unreachable fallthrough return in `fetchBody`

**Location:** `internal/web/fetcher.go:104`
**Confidence:** high

`fetchBody` loops `for attempt := 0; attempt < 2`. On `attempt=0`, every code path either `return`s (success or non-retryable error on line 97/102) or `continue`s to `attempt=1` (retryable + ctx alive, line 99-100). On `attempt=1`, the `attempt == 0` guard on line 99 is always false, so every code path `return`s (line 97 or 102). The loop body never lets the iterator reach 2, so line 104 (`return nil, nil, &WebError{…}`) is never executed.

This is not currently harmful because lines 97 and 102 cover every observable outcome, but the dead return hides the fact that the retry condition on line 99 also guards against "retry of a non-retryable error" — the structure is misleading and will silently stay wrong if someone adds a third attempt.

**Suggested fix:** Remove line 104. If the loop bound ever grows beyond 2, replace it with the explicit `return` inside the loop (e.g. last-iteration guard like `searxGet` does):

```go
func (c *Client) fetchBody(ctx context.Context, convID string, start *url.URL) ([]byte, *url.URL, error) {
    for attempt := 0; attempt < 2; attempt++ {
        body, finalURL, err := c.doHops(ctx, convID, start)
        if err == nil {
            return body, finalURL, nil
        }
        if errors.Is(err, errRetryable) && attempt == 0 && ctx.Err() == nil {
            continue
        }
        return nil, nil, c.classifyFetchErr(err)
    }
    // unreachable — remove this line
}
```

---

### [MEDIUM][DEAD-CODE] Unreachable fallthrough return in `searxGet`

**Location:** `internal/web/searxng.go:215`
**Confidence:** high

Same structural issue. `searxGet` loops `for attempt := 0; attempt < 2`. Every branch on `attempt=1` returns (lines 199, 206, 211, 213) — neither `continue` branch fires on `attempt=1` because both are guarded by `attempt == 1` which triggers a `return`. Line 215 (`return nil, errSearxUnreachable`) is therefore unreachable.

The comment on `decodeSearx` says the function closes the body; if line 215 were ever reached after a `continue` on attempt=1 (impossible today), the body would already be closed by `decodeSearx` — but since the line is unreachable, this is moot.

**Suggested fix:** Remove line 215 or replace the for-loop structure with an explicit two-attempt pattern that makes control flow obvious:

```go
// attempt 0
resp0, err := doAttempt(ctx, httpClient, endpoint, c.userAgent())
if err == nil { return resp0, nil }
if !isTransientFailure(err, ctx) { return nil, errSearxUnreachable }
// attempt 1
resp1, err := doAttempt(ctx, httpClient, endpoint, c.userAgent())
if err == nil { return resp1, nil }
return nil, errSearxUnreachable
```

---

### [LOW][BUG] Model-visible reason string outside the D-38 stable enum

**Location:** `internal/web/searxng.go:93`
**Confidence:** high

```go
return nil, &WebError{Code: CodeSearchUnavailable, Reason: "unsupported_category", …}
```

`errors.go` declares the stable reason constants under the comment "These strings are the model-visible `error` field values; they are a contract — never rename without a PRD amendment." The literal `"unsupported_category"` is used only here, is not declared as a constant, and appears in no test assertion. A future rename during a refactor would silently change the model-visible wire format without triggering the audit comment.

**Suggested fix:** Add `ReasonUnsupportedCategory = "unsupported_category"` to the constants block in `errors.go` and use it here.

---

### [LOW][BUG] `dnsPin` map grows without bound — expired entries never pruned

**Location:** `internal/web/dnspin.go:50-67`
**Confidence:** medium

`cache.get` (cache.go:83) lazy-evicts the expired in-memory entry on every read miss. `dnsPin.Pinned` (dnspin.go:54) checks expiry but does NOT delete the expired entry — it returns `(zero, false)` and leaves the stale `pinEntry` in the map. Every new `(conv, host)` pair that expires adds a permanent dead entry. For a long-running single-user agent with many conversations and varied hostnames, the map grows monotonically.

This is a low-severity memory leak; the single-user design and short TTL (default 60 s) bound the practical impact, but it is inconsistent with the `cache` eviction pattern in the same package.

**Suggested fix:** In `Pinned`, delete the expired entry before returning the miss:

```go
if !ok || !p.now().Before(e.expires) {
    if ok {
        delete(p.m, pinKey{conv, host}) // prune expired
    }
    return netip.Addr{}, false
}
```

---

## Checked and found clean

- **Resource leaks / body close:** `resp.Body` is closed in all branches of `doHops` (redirect path: line 128; non-redirect path: line 137) and `decodeSearx` (deferred). No leak path found.
- **Races:** All shared state (`cache.m`, `dnsPin.m`, `hostThrottle.m`) is guarded by `sync.Mutex`. The semaphore channel in `hostThrottle` is goroutine-safe by construction. No map written concurrently.
- **SSRF gate completeness:** `dialContext` calls `validateAndPin` before every dial; `control` re-checks the post-resolution IP in the production path; redirect hops are revalidated via `resolveRedirect` before the next dial. IPv4-mapped IPv6 collapse (`Unmap()`) is applied before classification.
- **Context propagation:** `convID` is stamped via `withConvID` in `doHops` for every hop; `resolveRedirect` passes `ctx` directly to `validateAndPin` (DNS-lookup context) with the explicit `convID` argument, which is correct.
- **`newGuard` third parameter:** `_ any` is a blank parameter, always `nil`. Mildly vestigial but harmless; golangci-lint does not flag blanked parameters.
- **Integer overflow in `gateAndRead`:** `int64(capBytes)+1` is safe on 64-bit targets; `len(body) > capBytes` cannot overflow.
- **Scheme-relative URLs:** `url.Parse("//host/path")` leaves `Scheme=""` which is rejected by `allowedSchemes` on line 58.
- **`Search` without SSRF gate:** Intentional — SearXNG is an in-network backend, not an arbitrary external host. The `maxSearxBodyBytes` cap provides defense-in-depth on the response.
- **Dead parameter `_ any` in `newGuard`:** Consistently `nil` everywhere, not a footgun. Flagged as informational only; excluded from the count.
