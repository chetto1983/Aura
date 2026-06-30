package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// The in-tree SAMPLE object-store sentinels (config.go:36-37). Shipping these under a
// strict profile is the F-007 credential-reuse EoP the validator rejects; the test sets
// them so gateObjectStoreCreds fires, AND uses them as redaction sentinels (T-33-10):
// their VALUES must never appear in the rendered report even though their knobs do.
const (
	sampleObjectStoreAccessKey = "GK000000000000000000000000"
	sampleObjectStoreSecretKey = "0000000000000000000000000000000000000000000000000000000000000000"
)

// setUnsafeServerProductionEnv composes an intentionally-unsafe server_production
// posture via t.Setenv: the required DB/Neo4j secrets ARE present so the run reaches
// the profile gates, but the sample object-store creds, an empty RPC secret, permissive
// CORS, a single replica, a disabled destructive-shell gate and an empty web-auth secret
// each trip a Fatal gate (plan 04). A loopback AGUI bind avoids an unrelated web-bind
// violation. loadBase() loads .env best-effort; t.Setenv values win (godotenv never
// overrides an already-set var), so the posture is deterministic on any host.
func setUnsafeServerProductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AURA_DB_URL", "postgres://u:p@h:5432/db") // required DB secret present
	t.Setenv("NEO4J_PASSWORD", "neo-secret")            // required graph secret present
	t.Setenv("AURA_AGUI_BIND", "127.0.0.1:9080")        // loopback ⇒ no web-bind violation
	t.Setenv("AURA_OBJECTSTORE_ACCESS_KEY", sampleObjectStoreAccessKey)
	t.Setenv("AURA_OBJECTSTORE_SECRET_KEY", sampleObjectStoreSecretKey)
	t.Setenv("GARAGE_RPC_SECRET", "")
	t.Setenv("AURA_AGUI_CORS_PERMISSIVE", "true")
	t.Setenv("AURA_OBJECTSTORE_REPLICATION_FACTOR", "1")
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", "off")
	t.Setenv("AURA_AUTHULA_SECRET", "")
	t.Setenv("AURA_PROFILE", "") // base profile irrelevant; --profile overrides it (D-02)
}

// offendingKnobs is the set of AURA_* knobs the unsafe server_production posture above
// must surface — criterion #1 / PROF-01: validate lists EVERY unmet requirement.
var offendingKnobs = []string{
	"AURA_OBJECTSTORE_ACCESS_KEY",
	"AURA_OBJECTSTORE_SECRET_KEY",
	"AURA_AGUI_CORS_PERMISSIVE",
	"AURA_OBJECTSTORE_REPLICATION_FACTOR",
	"GARAGE_RPC_SECRET",
	"AURA_AUTHULA_SECRET",
	"AURA_SHELL_DESTRUCTIVE_PATTERNS",
}

// TestConfigValidate_ServerProduction is the PROF-01 e2e: `aura config validate
// --profile server_production` exits non-zero listing every unmet requirement, each
// naming its knob; a benign --profile dev run exits 0; --json is CI-decodable; and no
// secret VALUE is ever echoed.
func TestConfigValidate_ServerProduction(t *testing.T) {
	t.Run("lists every unmet knob and exits 1", func(t *testing.T) {
		setUnsafeServerProductionEnv(t)
		var buf bytes.Buffer
		code := runConfigValidate([]string{"--profile", "server_production"}, &buf)
		if code != 1 {
			t.Fatalf("unsafe server_production must exit 1 (fail-closed), got %d; output:\n%s", code, buf.String())
		}
		out := buf.String()
		for _, knob := range offendingKnobs {
			if !strings.Contains(out, knob) {
				t.Errorf("validate output must name offending knob %s; got:\n%s", knob, out)
			}
		}
	})

	t.Run("benign --profile dev exits 0", func(t *testing.T) {
		// Required secrets present, NON-sample object-store creds, loopback bind: under
		// the lenient dev tier none of the strict gates fire (criterion #4 parity).
		t.Setenv("AURA_DB_URL", "postgres://u:p@h:5432/db")
		t.Setenv("NEO4J_PASSWORD", "neo-secret")
		t.Setenv("AURA_AGUI_BIND", "127.0.0.1:9080")
		t.Setenv("AURA_OBJECTSTORE_ACCESS_KEY", "GKrealoperatorkey0000000001")
		t.Setenv("AURA_OBJECTSTORE_SECRET_KEY", "realoperatorsecretvalue0000000000000000000000000000000000000000ab")
		t.Setenv("GARAGE_RPC_SECRET", "deadbeefrpcsecret")
		t.Setenv("AURA_AGUI_CORS_PERMISSIVE", "false")
		t.Setenv("AURA_OBJECTSTORE_REPLICATION_FACTOR", "1")
		t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", "")
		t.Setenv("AURA_AUTHULA_SECRET", "")
		t.Setenv("AURA_PROFILE", "")
		var buf bytes.Buffer
		code := runConfigValidate([]string{"--profile", "dev"}, &buf)
		if code != 0 {
			t.Fatalf("benign dev config must exit 0, got %d; output:\n%s", code, buf.String())
		}
	})

	t.Run("--json decodes into []config.Violation", func(t *testing.T) {
		setUnsafeServerProductionEnv(t)
		var buf bytes.Buffer
		code := runConfigValidate([]string{"--profile", "server_production", "--json"}, &buf)
		if code != 1 {
			t.Fatalf("server_production --json must still exit 1, got %d", code)
		}
		var violations []config.Violation
		if err := json.Unmarshal(buf.Bytes(), &violations); err != nil {
			t.Fatalf("--json output must decode into []config.Violation: %v; raw:\n%s", err, buf.String())
		}
		if len(violations) == 0 {
			t.Fatal("--json output must list the violations, got empty slice")
		}
		var names []string
		for _, v := range violations {
			names = append(names, v.Knob)
		}
		joined := strings.Join(names, ",")
		for _, knob := range offendingKnobs {
			if !strings.Contains(joined, knob) {
				t.Errorf("--json violations must include %s; got %v", knob, names)
			}
		}
	})

	t.Run("secret knob values are redacted", func(t *testing.T) {
		setUnsafeServerProductionEnv(t)
		var buf bytes.Buffer
		_ = runConfigValidate([]string{"--profile", "server_production"}, &buf)
		out := buf.String()
		// The secret KNOBS are named (their violations are rendered) ...
		if !strings.Contains(out, "AURA_OBJECTSTORE_SECRET_KEY") {
			t.Fatalf("expected the secret knob to be named in the report; got:\n%s", out)
		}
		// ... but their VALUES must never be echoed (T-33-10).
		for _, secretValue := range []string{sampleObjectStoreAccessKey, sampleObjectStoreSecretKey} {
			if strings.Contains(out, secretValue) {
				t.Errorf("secret VALUE %q must be redacted, but it appeared in:\n%s", secretValue, out)
			}
		}
	})
}
