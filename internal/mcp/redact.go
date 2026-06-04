package mcp

import "regexp"

var (
	envSecretRE    = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(TOKEN|SECRET|PASS|KEY|AUTH|BEARER|CREDENTIAL)[A-Z0-9_]*=)[^\s]+`)
	bearerSecretRE = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`)
)

// RedactSecrets masks secret-bearing values in a string before it is logged or
// surfaced in errors, replacing the values of token/secret/key-style env entries and
// Authorization: Bearer headers with <redacted>.
func RedactSecrets(s string) string {
	s = envSecretRE.ReplaceAllString(s, `${1}<redacted>`)
	return bearerSecretRE.ReplaceAllString(s, `${1}<redacted>`)
}
