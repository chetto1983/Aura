// Package secret holds the canonical secret-env-key predicate shared by every
// site that filters or redacts environment variables before they reach a child
// process or an exported config. Keeping ONE list prevents the divergence that
// let a bare *_KEY (e.g. PRIVATE_KEY) be redacted on one path and leak on
// another (audit finding B-09).
package secret

import (
	"regexp"
	"strings"
)

// secretEnvMarkers is the canonical, case-insensitive substring set marking an
// env key as sensitive. It is the safer SUPERSET of every pre-unification
// detector (shell child-env, MCP child-env, MCP managed-config export):
//   - "key"        subsumes api_key/apikey/private_key/ssh_key (the marker the
//     shell variant was missing, the B-09 leak).
//   - "pass"       subsumes password/passwd/passphrase.
//   - "private"/"cert" are added per the audit action plan (AP-13) so private
//     keys and certificates are never inherited by a child.
//
// Never shrink this list: redacting LESS than any prior site re-opens B-09.
var secretEnvMarkers = []string{
	"key",
	"token",
	"secret",
	"pass",
	"auth",
	"bearer",
	"credential",
	"private",
	"cert",
	"dsn",
	"conn",
	"pwd",
	"cookie",
	"session",
	"jwt",
	"url",
	"uri",
}

var alwaysSecretEnvMarkers = []string{
	"key",
	"token",
	"secret",
	"pass",
	"auth",
	"bearer",
	"credential",
	"private",
	"cert",
	"dsn",
	"conn",
	"pwd",
	"cookie",
	"session",
	"jwt",
}

var credentialURLKeyMarkers = []string{"url", "uri"}

var credentialURLPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^:/?#\s@]+:[^@/?#\s]+@`)

// IsSecretEnvKey reports whether an environment variable name looks sensitive
// (case-insensitive substring match against secretEnvMarkers). It is a
// best-effort hygiene denylist, not a security boundary: callers strip or mask
// matching keys before a child process or an exported profile can observe them.
func IsSecretEnvKey(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range secretEnvMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// IsSecretEnvVar reports whether a key/value pair should be withheld from a
// child process. It keeps IsSecretEnvKey's broad compatibility behavior while
// avoiding over-redaction of harmless URL config such as PUBLIC_URL. Credential-
// bearing URL values are always secret, even when the key name is innocuous.
func IsSecretEnvVar(name, value string) bool {
	lower := strings.ToLower(name)
	for _, marker := range alwaysSecretEnvMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if ContainsCredentialURL(value) {
		return true
	}
	for _, marker := range credentialURLKeyMarkers {
		if strings.Contains(lower, marker) {
			return dbURLKey(lower) && strings.TrimSpace(value) != ""
		}
	}
	return false
}

// ContainsCredentialURL reports whether s contains a URL/DSN authority with an
// explicit user:password component, for example postgres://user:pass@host/db.
func ContainsCredentialURL(s string) bool {
	return credentialURLPattern.MatchString(s)
}

func dbURLKey(lower string) bool {
	return strings.Contains(lower, "db_url") ||
		strings.Contains(lower, "database_url") ||
		strings.Contains(lower, "postgres_url") ||
		strings.Contains(lower, "neo4j_url")
}
