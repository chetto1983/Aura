# Audit: internal/web

**Verdict:** needs-work — three dead-code items confirmed; no bugs or races
**Counts:** critical 0 / high 0 / medium 1 / low 3

## Findings

### [MEDIUM][DEAD-CODE] `metadataV6Pfx` switch case is unreachable in `classify()`

**Location:** `internal/web/ssrf.go:55`
**Confidence:** high

`fd00:ec2::/32` is entirely contained within the ULA block `fd00::/8` (which itself is within `fc00::/7`). Go's `netip.Addr.IsPrivate()` returns `true` for any ULA address, so the `ip.IsPrivate()` case at line 46 always fires before the `metadataV6Pfx.Contains(ip)` case at line 55 for every possible input in the `fd00:ec2::/32` range. The branch at line 55-56 is provably unreachable for all valid inputs. The package-level variable `metadataV6Pfx` (`netip.MustParsePrefix("fd00:ec2::/32")`) is initialized at startup (causes a `MustParsePrefix` call in `init`) and checked on every `classify` invocation — with zero effect. This is acknowledged in `ssrf_test.go:39-41` and `.planning/phases/07-web-tools/07-04-SUMMARY.md` as the lone mutation-testing survivor.

The security posture is unaffected — the address is still blocked by `IsPrivate()`. The cost is a dead branch, an unnecessary `init`-time allocation, and a permanently surviving mutation.

**Suggested fix:** Remove the `metadataV6Pfx` variable and its `case metadataV6Pfx.Contains(ip)` switch arm. The `fd00:ec2::/32` range is fully covered by `IsPrivate()`. Update the test comment and the SUMMARY doc.

---

### [LOW][DEAD-CODE] `internalError.redirectFrom` field is never set in production code

**Location:** `internal/web/errors.go:93`, `internal/web/errors.go:109-110`
**Confidence:** high

`internalError` has an unexported `redirectFrom string` field (line 93). The `Error()` method branches on it (lines 109-110). However, no production `internalError` struct literal in `transport.go` (the only file that constructs `internalError` values) ever sets this field — all four call sites omit it (always zero). The branch at `errors.go:109` is dead in production. The field is only set in test fixtures (`ssrf_test.go:244`, `errors_test.go:40`).

Since `internalError` is unexported and its constructor is always a struct literal in `transport.go`, the field is provably never populated in production paths.

**Suggested fix:** Either wire `redirectFrom` in `resolveRedirect` (e.g., `redirectFrom: current.String() + " → " + next.String()`) so debug logs gain the redirect chain detail the field was designed to carry, OR remove the field entirely and its branch in `Error()` if the detail is not needed. The former is more useful.

---

### [LOW][DEAD-CODE] `internalError.statusCode` field is always zero in production; `sanitize()` copies a phantom value

**Location:** `internal/web/errors.go:88`, `internal/web/errors.go:139`
**Confidence:** high

`internalError` declares `statusCode int` (line 88). `sanitize()` copies it to `WebError.StatusCode` (line 139). No production `internalError` struct literal ever sets `statusCode` — all four call sites in `transport.go` omit it. Therefore `sanitize(ie).StatusCode` is always `0`, and the `StatusCode` copy in `sanitize()` is a no-op. The HTTP status code is only available in `gateAndRead` (fetcher.go:178), which constructs a `*WebError` directly — never an `internalError` — so the field never gets populated on the SSRF block path where `internalError` is used.

**Suggested fix:** Remove `statusCode int` from `internalError` and its copy from `sanitize()`. SSRF blocks have no HTTP status to report; the `WebError.StatusCode` field is populated by the direct `*WebError` path in `gateAndRead` independently.

---

### [LOW][NOT-WIRED] `newGuard`'s third parameter is always `nil`; no allowlist wired

**Location:** `internal/web/ssrf.go:75`
**Confidence:** high

`newGuard(res resolver, pin *dnsPin, _ any) *guard` accepts a third `any` parameter but immediately discards it (blank identifier). Every call site in production (`client.go:28`) and tests passes `nil`. The parameter signature implies a future allowlist or bypass hook was planned but never implemented. The discarded slot adds noise to the constructor API with no benefit.

**Suggested fix:** Remove the `_ any` parameter from `newGuard` and update all call sites to pass only `res` and `pin`. If an allowlist is needed in a future slice, add it then.

## What Was Checked and Found Clean

- **Nil-pointer dereference:** `art.Node == nil` is explicitly guarded in `html.go:59` before `convertNode`. `url.Parse` results are always checked. No unsafe pointer dereferences found.
- **Resource leaks:** Every `resp.Body` is closed exactly once on all paths in `doHops` (redirect path: line 128; non-redirect path: line 137). `gateAndRead` does not close the body; the caller does. `decodeSearx` uses `defer resp.Body.Close()` correctly. No leaked goroutines (confirmed by goleak harness in `main_test.go`).
- **Context propagation:** `context.WithTimeout` is used in `Fetch` and `Search`; `cancel()` is always deferred. The conversation ID is propagated via `withConvID`/`convIDFrom` through the request context. No missing cancellation.
- **Race conditions:** `dnsPin` and `cache` both use `sync.Mutex` guarding their maps on all read and write paths. `hostThrottle` uses a mutex for lazy map creation. No concurrent map access without lock.
- **JSON/error handling:** `json.Unmarshal` return values are always checked in `fetchFromCache`. `json.Marshal` errors in `fetchToCache` and the search cache path are silently skipped — intentional per the "cache is an optimization, never a correctness dependency" invariant.
- **Retry logic:** The one-retry loop in `fetchBody` and `searxGet` is correctly bounded at 2 attempts; a cancelled context short-circuits before the second attempt.
- **SSRF classifier:** `ip.Unmap()` runs before the switch in `classify()`, closing the IPv4-mapped-IPv6 bypass (Pitfall 2). Mixed-record sets fail closed (all records must pass). Hostname blocklist checked before resolution. All expected classes (loopback, private, link-local, CGNAT, unspecified, multicast, this-network) are blocked.
- **`searxGet` unguarded transport:** Uses a plain `http.Client` instead of the hardened transport. This is intentional design — SearXNG is an operator-configured in-network service (D-02/D-03, comment at searxng.go:237). The endpoint is config-controlled, not user/model-controlled, so SSRF is not in scope for this hop. Not flagged as a bug.
- **Integer conversion:** `int64(capBytes)+1` in `gateAndRead` is safe since `capBytes` is read from `cfg.WebFetchMaxBodyBytes` which is a normal int; no overflow risk in practice.
