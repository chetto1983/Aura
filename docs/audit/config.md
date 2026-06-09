# Audit: internal/config

**Verdict:** needs-work — two confirmed defects: a not-wired config field and a DSN injection via unescaped query/authority components.

**Counts:** critical 0 / high 1 / medium 1 / low 0

---

## Findings

### [HIGH][NOT-WIRED] `HistoryHardCapTurns` parsed and stored but never consumed

**Location:** `internal/config/config.go:54,233`
**Confidence:** high

`Config.HistoryHardCapTurns` (env `AURA_HISTORY_HARD_CAP_TURNS`, default 50) is declared in the struct (line 54) and populated via `envIntDefault` (line 233). A `grep` across the entire repo (`D:/Aura/**/*.go`) finds zero non-definition, non-test reads of this field:

```
internal/config/config.go:54  HistoryHardCapTurns int   // definition
internal/config/config.go:233 HistoryHardCapTurns: ...  // assignment in loadBase
```

No other `.go` file reads `cfg.HistoryHardCapTurns`. The comment says "L2.5 picobot hard rolling buffer cap", but `internal/conversations/context.go` computes its hard cap from `ContextWindow − max(MaxOutputTokens, 20 000) − 13 000` (token budget, not turn count), and `internal/runner/runner.go` never passes a `HardCapTurns` field anywhere. The `.planning/phases/04-VERIFICATION.md` lists the field as "VERIFIED" for having a default, but verification of the default value is not verification of the field being consumed downstream.

**Impact:** the env knob `AURA_HISTORY_HARD_CAP_TURNS` silently does nothing; operators who tune it believe they are capping rolling history when they are not.

**Suggested fix:** Either (a) wire the field into `runner.Deps` and thread it through to the L2.5 drop loop in `conversations.ApplyContextLadder`, or (b) remove the field and the env knob entirely and document that the rolling cap is purely token-budget-driven.

---

### [MEDIUM][BUG] `composeDSN` does not escape `host`, `port`, or `sslmode`

**Location:** `internal/config/config.go:293-299`
**Confidence:** high

```go
return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
    url.QueryEscape(role),
    url.QueryEscape(password),
    host, port,              // NOT escaped
    url.QueryEscape(dbname),
    sslmode,                 // NOT query-escaped
)
```

`role`, `password`, and `dbname` are correctly `url.QueryEscape`d. `host`, `port`, and `sslmode` are interpolated raw into the DSN string.

**Concrete injections:**

- `POSTGRES_SSLMODE=disable&connect_timeout=0` → DSN becomes `…?sslmode=disable&connect_timeout=0`, injecting an extra connection parameter.
- `POSTGRES_HOST=evil@real-host` → DSN becomes `postgres://role:pass@evil@real-host:5432/db?…`, confusing the URL authority parser (the `@` demarcates userinfo from host; the parser sees `real-host` as the host and `role:pass@evil` as userinfo, breaking auth).
- `POSTGRES_PORT=5432/extra` → injects a path segment.

All three values are operator-supplied via environment variables, so this is an operator misconfiguration risk rather than an external attacker path. However, the current code trusts the operator to never include URL-significant chars in these fields, which is not enforced and is not documented in comments.

**Suggested fix:**

```go
import "net/url"

func composeDSN(role, password, host, port, dbname, sslmode string) string {
    if password == "" {
        return ""
    }
    u := &url.URL{
        Scheme: "postgres",
        User:   url.UserPassword(role, password),
        Host:   net.JoinHostPort(host, port),
        Path:   "/" + url.PathEscape(dbname),
        RawQuery: url.Values{"sslmode": {sslmode}}.Encode(),
    }
    return u.String()
}
```

Using `url.URL` struct construction guarantees all components are correctly encoded by the standard library. `net.JoinHostPort` handles IPv6 brackets. Alternatively, at minimum, `sslmode` must be `url.QueryEscape`d and `host`/`port` validated to contain no URL-significant characters before interpolation.
