package secret

import (
	"os"
	"sort"
	"strings"
	"sync/atomic"
)

// ConfiguredValuePlaceholder is persisted in place of Aura's own configured
// secret values at inbound at-rest boundaries.
const ConfiguredValuePlaceholder = "[REDACTED_CONFIGURED_SECRET]"

// configuredSecretMinBytes prevents short configured values from corrupting
// unrelated working data through substring replacement. Twelve bytes keeps
// collisions negligible while covering real credentials and DSNs.
const configuredSecretMinBytes = 12

// ExactRedactor is the inbound configured-secret detector. It intentionally
// differs from outbound pattern redaction: only exact values harvested from
// Aura's environment are replaced, so agent-discovered working data survives.
type ExactRedactor struct {
	values []string
}

var activeExactRedactor atomic.Pointer[ExactRedactor]

func init() {
	ConfigureExactRedactor(os.Environ())
}

// NewExactRedactor constructs a deterministic configured-secret snapshot from
// an environment vector. Values are de-duplicated and longest-first so an
// overlapping shorter secret cannot expose the suffix of a longer one.
func NewExactRedactor(environ []string) *ExactRedactor {
	seen := make(map[string]struct{})
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || len(value) < configuredSecretMinBytes || !IsSecretEnvVar(name, value) {
			continue
		}
		seen[value] = struct{}{}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) != len(values[j]) {
			return len(values[i]) > len(values[j])
		}
		return values[i] < values[j]
	})
	return &ExactRedactor{values: values}
}

// ConfigureExactRedactor atomically replaces the process-scoped snapshot. The
// config loader calls this after loading .env, so both inherited and dotenv
// secrets are covered; production normally performs this once at boot.
func ConfigureExactRedactor(environ []string) {
	activeExactRedactor.Store(NewExactRedactor(environ))
}

// RedactConfigured replaces Aura's configured secret values while preserving
// all unknown values, including secrets discovered by the agent at runtime.
func RedactConfigured(s string) string {
	redactor := activeExactRedactor.Load()
	if redactor == nil {
		return s
	}
	return redactor.Redact(s)
}

// Redact applies exact configured-value replacement.
func (r *ExactRedactor) Redact(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, value := range r.values {
		s = strings.ReplaceAll(s, value, ConfiguredValuePlaceholder)
	}
	return s
}
