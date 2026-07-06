// docker_backend_exec_test.go unit-proves the box exec env scrub (T-37-04-SECRETENV) without
// a live dockerd: scrubEnv must drop secret-like KEY=VALUE pairs exactly as the host mergeEnv
// path does (secret.IsSecretEnvVar) so no host secret crosses into an untrusted box.

package usersandbox

import (
	"slices"
	"testing"
)

func TestExec_ScrubsSecretEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/root",
		"PUBLIC_URL=https://example.com",
		"OPENROUTER_API_KEY=sk-secret",
		"AWS_SECRET_ACCESS_KEY=abc123",
		"GITHUB_TOKEN=ghp_xxx",
		"DB_PASSWORD=hunter2",
		"SESSION_ID=deadbeef",
		"DATABASE_URL=postgres://user:pass@host:5432/db",
		"MALFORMED_NO_EQUALS",
	}
	got := scrubEnv(in)

	wantKept := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/root",
		"PUBLIC_URL=https://example.com",
	}
	for _, kv := range wantKept {
		if !slices.Contains(got, kv) {
			t.Errorf("scrubEnv dropped non-secret %q; got %v", kv, got)
		}
	}

	wantDropped := []string{
		"OPENROUTER_API_KEY=sk-secret",
		"AWS_SECRET_ACCESS_KEY=abc123",
		"GITHUB_TOKEN=ghp_xxx",
		"DB_PASSWORD=hunter2",
		"SESSION_ID=deadbeef",
		"DATABASE_URL=postgres://user:pass@host:5432/db",
		"MALFORMED_NO_EQUALS",
	}
	for _, kv := range wantDropped {
		if slices.Contains(got, kv) {
			t.Errorf("scrubEnv leaked secret/invalid entry %q; got %v", kv, got)
		}
	}
}
