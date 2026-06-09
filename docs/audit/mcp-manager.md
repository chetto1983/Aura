# Audit: internal/mcp/manager

**Verdict:** needs-work — three dead-code / not-wired findings; no bugs or races.

**Counts:** critical 0 / high 0 / medium 2 / low 1

---

## Findings

### [MEDIUM][DEAD-CODE] StartupStarting / StartupReady / StartupFailed never produced

**Location:** `internal/mcp/manager/status.go:13-15`

**Confidence:** high

**Detail:**
`SnapshotStatus` initialises `state` to `StartupUnknown` and transitions only to
`StartupDisabled` or `StartupBlocked`. The three constants `StartupStarting`,
`StartupReady`, and `StartupFailed` are never assigned to any `StatusSnapshot.StartupState`
in this file, and a repo-wide search confirms they appear nowhere else in non-test
production code. Consumers that switch on `StartupState` values (e.g. a future UI)
will never observe these three states.

**Suggested fix:**
Either (a) remove the three constants if static status is the design intent, or
(b) wire actual process-lifecycle tracking (start/ready/failed signals from the
`mcp.Client` lifecycle) so the constants carry real meaning.

---

### [MEDIUM][NOT-WIRED] ExportProfile / ImportProfile / ImportOptions / RedactEnv have no production callers

**Location:** `internal/mcp/manager/config.go:19, 46, 13, 77`

**Confidence:** high

**Detail:**
`ExportProfile`, `ImportProfile`, `ImportOptions`, and `RedactEnv` are exported
symbols with full implementations and test coverage, but grep across all non-test
`.go` files in the repo reveals zero call sites outside the package's own `*_test.go`
files. The `aura mcp profile` command (`cmd/aura/mcp_profile.go`) handles profiles
without using these functions; there is no `aura mcp profile export|import` subcommand.
`RedactEnv` is called internally only by `ExportProfile` — if `ExportProfile` is dead,
`RedactEnv` is also dead outside the package.

**Suggested fix:**
Wire `ExportProfile` / `ImportProfile` to an `aura mcp profile export|import`
subcommand (as the PRD profile-sharing use-case implies), or move the functions to
`internal` (unexported) helpers until a caller exists. Keeping exported symbols with
no callers inflates the package surface and misleads auditors.

---

### [LOW][DEAD-CODE] Redundant branch in mergeEnvPreserveCredentials collapses to a single condition

**Location:** `internal/mcp/manager/config.go:117-124`

**Confidence:** high

**Detail:**
The two consecutive `if` blocks:

```go
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && isPlaceholderValue(key, value) {
    out = append(out, prior)
    continue
}
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && !isPlaceholderValue(key, value) {
    out = append(out, prior)
    continue
}
```

are logically equivalent to the single condition `ok && isSecretEnvKey(key)` because
`isPlaceholderValue` and `!isPlaceholderValue` partition the full boolean domain: one
of the two `if` bodies always executes when `ok && isSecretEnvKey(key)`. The second
`existingByKey[key]` lookup is redundant. The code is correct — the prior always wins
for a secret key — but the distinction introduced by `isPlaceholderValue` is inert,
which is misleading (a reader may think the two cases produce different output).

**Suggested fix:**
Collapse to one branch:
```go
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) {
    out = append(out, prior)
    continue
}
```

---

## What was checked and found clean

- **Nil-pointer / panic:** All map accesses use the two-value form or are guarded by
  `ProfileServerNames` which pre-filters to known keys. No unchecked indexing.
- **Unchecked errors:** `ImportProfile` always returns `nil` — the signature is
  consistent (no error path exists in the body). Callers check the error correctly.
- **Resource leaks / goroutines:** The package is pure computation (no goroutines,
  no I/O handles, no timers). Nothing to leak.
- **Races:** No shared mutable state; all functions accept value or pointer parameters
  with no background goroutines.
- **Context propagation:** Not applicable — no I/O paths in this package.
- **Slice aliasing:** `dockerRuntimeConfig` builds `env` with `append([]string(nil), server.Env...)` — correct defensive copy. `mergeEnvPreserveCredentials` similarly copies.
- **errMCPServerBlocked wrapping:** `fmt.Errorf("%w: ...", errMCPServerBlocked)` is
  correct; callers use `errors.Is`, which unwraps correctly.
- **Docker env forwarding:** The `-e KEY` / `cmd.Env` split is intentional and correct:
  `cfg.Env` (`KEY=VALUE`) is appended to the Docker process env via `mcp.Open`
  (`cmd.Env = append(os.Environ(), cfg.Env...)`), and Docker's own `-e KEY` (no value)
  reads from that process env — so container sees the value. Not a bug.
- **Trust fallthrough:** `normalizedTrustForServer` defaults to `TrustBlocked` for
  unknown sources — correct fail-safe.
