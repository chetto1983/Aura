package agui

import (
	"net/url"
	"strings"
)

// governance_write_skills_redact.go sanitizes a skill INSTALL SOURCE before it crosses the
// wire or is echoed back to the operator. A source can be a bare `owner/repo`, a full URL,
// or a URL carrying credentials — e.g. `https://host/repo?token=SECRET`,
// `https://user:pass@host/repo`, or `owner/repo?token=SECRET`. The Phase-29 SPEC Prohibition
// #1 ("no raw secret anywhere") requires the install response (SkillsInstallInfo.Source) to
// never carry such a secret value; the held-out secret-scan (governance_write_secret_scan_test.go)
// catches a regression. This is the single redaction chokepoint for the source echo.

// secretQueryKeys names the query-parameter keys whose VALUE is a credential and must be
// masked in any echoed source. It mirrors the canonical secret-marker substrings used by
// internal/secret (key/token/secret/pass/auth/...), matched case-insensitively as a substring
// so e.g. `access_token`, `api_key`, `private_token` all redact.
var secretQueryMarkers = []string{
	"token", "secret", "pass", "key", "auth", "bearer", "credential", "sig", "signature",
}

// sanitizeSkillSource returns source with any embedded credential masked: userinfo in a URL
// authority (`user:pass@`) becomes `user:<redacted>@` (or `<redacted>@` for a bare token),
// and any query-parameter whose key marks a secret has its value replaced with `<redacted>`.
// A non-URL `owner/repo` (no scheme) is still scanned for a `?key=value` credential tail so a
// `owner/repo?token=SECRET` shorthand is covered. The source's non-secret shape is preserved
// so the operator still recognizes what they installed.
func sanitizeSkillSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return source
	}

	// Split off a query tail first so the bare `owner/repo?token=X` shorthand (no scheme,
	// not a parseable URL authority) is still redacted.
	base, query, hasQuery := strings.Cut(source, "?")
	if hasQuery {
		query = redactSecretQuery(query)
	}

	base = redactURLUserinfo(base)

	if hasQuery {
		return base + "?" + query
	}
	return base
}

// redactURLUserinfo masks credentials embedded in a URL authority. It parses the base as a
// URL; if it has userinfo with a password (or a single opaque token treated as a password),
// the secret half is replaced with <redacted>. A string that does not parse as a URL with a
// host is returned unchanged (a bare `owner/repo` has no authority to redact).
func redactURLUserinfo(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.User == nil {
		return base
	}
	name := u.User.Username()
	if _, hasPass := u.User.Password(); hasPass {
		u.User = url.UserPassword(name, "<redacted>")
	} else if name != "" {
		// A single opaque userinfo token (e.g. an x-access-token PAT) is itself the secret.
		u.User = url.User("<redacted>")
	}
	return u.String()
}

// redactSecretQuery masks the value of every secret-marked query parameter, preserving
// parameter order and the non-secret pairs. It manually splits on `&`/`=` (rather than
// url.ParseQuery → Encode, which reorders and percent-re-encodes) so the echoed source stays
// readable.
func redactSecretQuery(query string) string {
	pairs := strings.Split(query, "&")
	for i, pair := range pairs {
		key, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		if isSecretQueryKey(key) {
			pairs[i] = key + "=<redacted>"
		}
	}
	return strings.Join(pairs, "&")
}

// isSecretQueryKey reports whether a query-parameter key marks a secret value (substring,
// case-insensitive) — the same predicate shape as internal/secret.IsSecretEnvKey.
func isSecretQueryKey(key string) bool {
	lower := strings.ToLower(key)
	for _, m := range secretQueryMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
