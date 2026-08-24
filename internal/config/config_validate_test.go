package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
)

// TestConfigValidate covers O-04: required infra secrets are checked at boot so a
// misconfigured deploy fails fast with a named error instead of a late cryptic
// auth failure.
//
// The full() fixture is a realistic LOADED Config (loopback AGUIBind, Profile dev,
// replication 1) rather than the old bare {DB}: Validate() is now the unified
// profile-aware contract (it runs the full gate aggregation, not just the two secret
// checks), so the fixture must reflect a real loaded shape that validates clean under
// the default lenient tier.
func TestConfigValidate(t *testing.T) {
	full := func() *Config {
		return &Config{
			DB:                           db.Config{URL: "postgres://u:p@h:5432/db"},
			Profile:                      ProfileDev,
			AGUIBind:                     "127.0.0.1:9080",
			ObjectStoreReplicationFactor: 1,
		}
	}
	if err := full().Validate(); err != nil {
		t.Fatalf("fully-configured Config must validate, got %v", err)
	}

	c := full()
	c.DB.URL = ""
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatalf("empty DB URL (no POSTGRES_PASSWORD / AURA_DB_URL) must fail validation naming the var, got %v", err)
	}
}

func TestConfigValidateRejectsNonPositiveHistoryHardCap(t *testing.T) {
	t.Setenv("AURA_HISTORY_HARD_CAP_TURNS", "0")
	cfg := &Config{
		DB:                           db.Config{URL: "postgres://u:p@h:5432/db"},
		Profile:                      ProfileServerProduction,
		AGUIBind:                     "127.0.0.1:9080",
		ObjectStoreReplicationFactor: 2,
	}
	violations := cfg.ValidateProfile(ProfileServerProduction)
	if !hasViolation(violations, "AURA_HISTORY_HARD_CAP_TURNS", Fatal) {
		t.Fatalf("non-positive history cap violations = %#v", violations)
	}
}

// hasViolation reports whether vs contains a Violation naming knob at severity sev.
func hasViolation(vs []Violation, knob string, sev Severity) bool {
	for _, v := range vs {
		if v.Knob == knob && v.Sev == sev {
			return true
		}
	}
	return false
}

// allProfiles is the full tier set the gate tables iterate; Strict() partitions it
// into lenient {dev, local_trusted} and strict {single_user_hardened, server_production}.
var allProfiles = []RuntimeProfile{
	ProfileDev, ProfileLocalTrusted, ProfileSingleUserHardened, ProfileServerProduction,
}

// TestGateObjectStore locks F-007/PROF-03: the in-tree SAMPLE object-store creds are
// Fatal under the strict tiers (each naming its knob) and OK under dev/local_trusted;
// real creds are never flagged.
func TestGateObjectStore(t *testing.T) {
	sample := &Config{
		ObjectStoreAccessKey: defaultObjectStoreAccessKey,
		ObjectStoreSecretKey: defaultObjectStoreSecretKey,
	}
	real := &Config{
		ObjectStoreAccessKey: "GKrealoperatorkey0000000001",
		ObjectStoreSecretKey: "realoperatorsecretvalue000000000000000000000000000000000000000000",
	}
	for _, p := range allProfiles {
		t.Run(string(p), func(t *testing.T) {
			vs := sample.gateObjectStoreCreds(p)
			if p.Strict() {
				if !hasViolation(vs, "AURA_OBJECTSTORE_ACCESS_KEY", Fatal) {
					t.Errorf("sample access key under %s must be Fatal naming AURA_OBJECTSTORE_ACCESS_KEY, got %+v", p, vs)
				}
				if !hasViolation(vs, "AURA_OBJECTSTORE_SECRET_KEY", Fatal) {
					t.Errorf("sample secret key under %s must be Fatal naming AURA_OBJECTSTORE_SECRET_KEY, got %+v", p, vs)
				}
			} else if len(vs) != 0 {
				t.Errorf("lenient tier %s must not flag sample creds, got %+v", p, vs)
			}
			if rv := real.gateObjectStoreCreds(p); len(rv) != 0 {
				t.Errorf("real creds under %s must yield no violation, got %+v", p, rv)
			}
		})
	}
}

// TestGateGarageRPCSecret locks F-007/A5: an empty GARAGE_RPC_SECRET is Fatal under the
// strict tiers; a set secret and the lenient tiers yield nothing.
func TestGateGarageRPCSecret(t *testing.T) {
	for _, p := range allProfiles {
		t.Run(string(p), func(t *testing.T) {
			empty := (&Config{GarageRPCSecret: "  "}).gateGarageRPCSecret(p)
			if p.Strict() {
				if !hasViolation(empty, "GARAGE_RPC_SECRET", Fatal) {
					t.Errorf("empty RPC secret under %s must be Fatal naming GARAGE_RPC_SECRET, got %+v", p, empty)
				}
			} else if len(empty) != 0 {
				t.Errorf("lenient tier %s must not flag an empty RPC secret, got %+v", p, empty)
			}
			if set := (&Config{GarageRPCSecret: "deadbeef"}).gateGarageRPCSecret(p); len(set) != 0 {
				t.Errorf("set RPC secret under %s must yield no violation, got %+v", p, set)
			}
		})
	}
}

// TestGateReplication is the hardened↔prod differentiator (D-11/PROF-06/F-018):
// server_production rejects replication < 2; single_user_hardened ALLOWS 1; the
// lenient tiers never constrain it.
func TestGateReplication(t *testing.T) {
	if vs := (&Config{ObjectStoreReplicationFactor: 1}).gateReplication(ProfileServerProduction); !hasViolation(vs, "AURA_OBJECTSTORE_REPLICATION_FACTOR", Fatal) {
		t.Fatalf("server_production with replication=1 must be Fatal naming AURA_OBJECTSTORE_REPLICATION_FACTOR, got %+v", vs)
	}
	if vs := (&Config{ObjectStoreReplicationFactor: 2}).gateReplication(ProfileServerProduction); len(vs) != 0 {
		t.Errorf("server_production with replication=2 must pass, got %+v", vs)
	}
	for _, p := range []RuntimeProfile{ProfileDev, ProfileLocalTrusted, ProfileSingleUserHardened} {
		if vs := (&Config{ObjectStoreReplicationFactor: 1}).gateReplication(p); len(vs) != 0 {
			t.Errorf("%s must ALLOW replication=1 (only prod requires >=2), got %+v", p, vs)
		}
	}
}

// TestGateDestructiveShell locks D-11/D-15/F-002: explicit `off` is Fatal under
// server_production ONLY (single_user_hardened ALLOWS it, A3); it reads the raw env
// value (case-insensitive) and never imports the tools leaf.
func TestGateDestructiveShell(t *testing.T) {
	tests := []struct {
		name      string
		profile   RuntimeProfile
		env       string
		wantFatal bool
	}{
		{name: "prod off lowercase", profile: ProfileServerProduction, env: "off", wantFatal: true},
		{name: "prod OFF uppercase", profile: ProfileServerProduction, env: "OFF", wantFatal: true},
		{name: "prod Off padded", profile: ProfileServerProduction, env: "  Off  ", wantFatal: true},
		{name: "prod custom patterns", profile: ProfileServerProduction, env: "rm -rf /,mkfs", wantFatal: false},
		{name: "prod empty keeps gate", profile: ProfileServerProduction, env: "", wantFatal: false},
		{name: "hardened allows off", profile: ProfileSingleUserHardened, env: "off", wantFatal: false},
		{name: "dev allows off", profile: ProfileDev, env: "off", wantFatal: false},
		{name: "local_trusted allows off", profile: ProfileLocalTrusted, env: "off", wantFatal: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", tc.env)
			vs := (&Config{}).gateDestructiveShell(tc.profile)
			got := hasViolation(vs, "AURA_SHELL_DESTRUCTIVE_PATTERNS", Fatal)
			if got != tc.wantFatal {
				t.Errorf("gateDestructiveShell(%s) env=%q: Fatal=%v, want %v (%+v)", tc.profile, tc.env, got, tc.wantFatal, vs)
			}
		})
	}
}

// TestGateReasoningTraceFull locks SEC-01/D-12: detailed trace remains available
// to lenient local profiles, while strict profiles require an explicit operator
// acknowledgement before boot.
func TestGateReasoningTraceFull(t *testing.T) {
	tests := []struct {
		name      string
		profile   RuntimeProfile
		mode      string
		ack       string
		wantFatal bool
	}{
		{name: "dev full is a no-op", profile: ProfileDev, mode: "full", wantFatal: false},
		{name: "local trusted full is a no-op", profile: ProfileLocalTrusted, mode: "full", wantFatal: false},
		{name: "hardened full without ack", profile: ProfileSingleUserHardened, mode: "full", wantFatal: true},
		{name: "production full without ack", profile: ProfileServerProduction, mode: " full ", wantFatal: true},
		{name: "production full with ack", profile: ProfileServerProduction, mode: "FULL", ack: "1", wantFatal: false},
		{name: "production summary needs no ack", profile: ProfileServerProduction, mode: "summary", wantFatal: false},
		{name: "production off needs no ack", profile: ProfileServerProduction, mode: "off", wantFatal: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_REASONING_TRACE", tc.mode)
			t.Setenv("AURA_TRACE_FULL_ACK", tc.ack)
			vs := (&Config{}).gateReasoningTraceFull(tc.profile)
			got := hasViolation(vs, "AURA_REASONING_TRACE", Fatal)
			if got != tc.wantFatal {
				t.Fatalf("gateReasoningTraceFull(%s), mode=%q ack=%q: Fatal=%v, want %v (%+v)",
					tc.profile, tc.mode, tc.ack, got, tc.wantFatal, vs)
			}
		})
	}
}

// TestGateWebAuth locks D-10/D-11: an empty AURA_AUTHULA_SECRET is Fatal under the
// strict tiers and OK under the lenient ones.
func TestGateWebAuth(t *testing.T) {
	for _, p := range allProfiles {
		t.Run(string(p), func(t *testing.T) {
			empty := (&Config{AuthulaSecret: ""}).gateWebAuth(p)
			if p.Strict() {
				if !hasViolation(empty, "AURA_AUTHULA_SECRET", Fatal) {
					t.Errorf("empty web-auth secret under %s must be Fatal naming AURA_AUTHULA_SECRET, got %+v", p, empty)
				}
			} else if len(empty) != 0 {
				t.Errorf("lenient tier %s must not require web-auth, got %+v", p, empty)
			}
			if set := (&Config{AuthulaSecret: "32byteshexsecretvalueforauthula0"}).gateWebAuth(p); len(set) != 0 {
				t.Errorf("set web-auth secret under %s must yield no violation, got %+v", p, set)
			}
		})
	}
}

// TestGateMultiUserNeedsASandbox pins the one combination that makes every other
// isolation control decorative: a second identity on a deployment whose tools run on the
// host. Four planes are shared and unscoped — the skills library, host shell/fs,
// aura.settings (which holds OPENROUTER_API_KEY) and the MCP catalog — so host access
// defeats the five planes that ARE isolated.
//
// The gate deliberately forbids the PAIR, not either half: running on the host is the
// product for a single-principal appliance, and multi-user is fine once tools route into
// a per-identity box.
func TestGateMultiUserNeedsASandbox(t *testing.T) {
	t.Parallel()

	// The dangerous pair, at every non-strict profile.
	for _, p := range []RuntimeProfile{ProfileDev, ProfileLocalTrusted} {
		vs := (&Config{MUSRIsolation: true}).gateMultiUserNeedsASandbox(p)
		if !hasViolation(vs, "AURA_MUSR_ISOLATION", Fatal) {
			t.Errorf("multi-user under %s must be Fatal, got %+v", p, vs)
		}
	}

	// Strict profiles route tools into a per-identity box, so the pair is safe.
	for _, p := range []RuntimeProfile{ProfileSingleUserHardened, ProfileServerProduction} {
		if vs := (&Config{MUSRIsolation: true}).gateMultiUserNeedsASandbox(p); len(vs) != 0 {
			t.Errorf("multi-user under strict %s must pass, got %+v", p, vs)
		}
	}

	// Single-principal on the host is the SHIPPED product, not a violation. This
	// assertion is the one that stops someone "hardening" the gate into refusing the
	// operator's own appliance.
	if vs := (&Config{MUSRIsolation: false}).gateMultiUserNeedsASandbox(ProfileDev); len(vs) != 0 {
		t.Errorf("single-principal dev must pass untouched, got %+v", vs)
	}
	if vs := (*Config)(nil).gateMultiUserNeedsASandbox(ProfileDev); len(vs) != 0 {
		t.Errorf("nil config must not violate, got %+v", vs)
	}
}

// TestGateObjectStoreEndpoint locks A6: default bucket + loopback endpoint under
// server_production are WARN (never Fatal), and only under production.
func TestGateObjectStoreEndpoint(t *testing.T) {
	def := &Config{ObjectStoreBucket: "aura-assets", ObjectStoreEndpoint: "http://127.0.0.1:3900"}
	vs := def.gateObjectStoreEndpoint(ProfileServerProduction)
	if !hasViolation(vs, "AURA_OBJECTSTORE_BUCKET", Warn) {
		t.Errorf("default bucket under prod must WARN naming AURA_OBJECTSTORE_BUCKET, got %+v", vs)
	}
	if !hasViolation(vs, "AURA_OBJECTSTORE_ENDPOINT", Warn) {
		t.Errorf("loopback endpoint under prod must WARN naming AURA_OBJECTSTORE_ENDPOINT, got %+v", vs)
	}
	for _, v := range vs {
		if v.Sev == Fatal {
			t.Errorf("endpoint/bucket defaults must never be Fatal, got %+v", v)
		}
	}
	if custom := (&Config{ObjectStoreBucket: "prod-assets", ObjectStoreEndpoint: "https://garage.prod:3900"}).gateObjectStoreEndpoint(ProfileServerProduction); len(custom) != 0 {
		t.Errorf("custom bucket+endpoint under prod must yield no violation, got %+v", custom)
	}
	if hardened := def.gateObjectStoreEndpoint(ProfileSingleUserHardened); len(hardened) != 0 {
		t.Errorf("endpoint WARNs are prod-only, hardened must yield nothing, got %+v", hardened)
	}
}

// TestGateWebBind locks the reuse of GuardWebBind as a Violation: a non-loopback bind
// without auth is Fatal naming AURA_AGUI_BIND (all tiers); loopback and authed binds pass.
func TestGateWebBind(t *testing.T) {
	if vs := (&Config{AGUIBind: "0.0.0.0:9080"}).gateWebBind(); !hasViolation(vs, "AURA_AGUI_BIND", Fatal) {
		t.Fatalf("non-loopback bind without auth must be Fatal naming AURA_AGUI_BIND, got %+v", vs)
	}
	if vs := (&Config{AGUIBind: "127.0.0.1:9080"}).gateWebBind(); len(vs) != 0 {
		t.Errorf("loopback bind must pass, got %+v", vs)
	}
	if vs := (&Config{AGUIBind: "192.168.1.10:9080", AuthulaSecret: "secret"}).gateWebBind(); len(vs) != 0 {
		t.Errorf("authed non-loopback bind must pass, got %+v", vs)
	}
}

// TestGateRequiredSecrets locks the all-tier required-secret gate (O-04): an empty DB
// DSN is Fatal naming its knob; a set DSN yields nothing. The DB DSN is the only entry
// — a secret no code path reads must never be able to refuse a boot.
func TestGateRequiredSecrets(t *testing.T) {
	if vs := (&Config{}).gateRequiredSecrets(); !hasViolation(vs, "POSTGRES_PASSWORD (or AURA_DB_URL)", Fatal) {
		t.Errorf("empty DB URL must be Fatal naming POSTGRES_PASSWORD, got %+v", vs)
	}
	full := &Config{DB: db.Config{URL: "postgres://u:p@h/db"}}
	if vs := full.gateRequiredSecrets(); len(vs) != 0 {
		t.Errorf("a set DB DSN must yield no violation, got %+v", vs)
	}
}

// TestGateRunDir locks F-041/PROF-05: a non-absolute RunDir (or a load-time RunDirErr)
// is Fatal naming AURA_RUN_DIR in every tier; an absolute RunDir passes.
func TestGateRunDir(t *testing.T) {
	if vs := (&Config{RunDir: "relative/runs"}).gateRunDir(); !hasViolation(vs, "AURA_RUN_DIR", Fatal) {
		t.Errorf("non-absolute RunDir must be Fatal naming AURA_RUN_DIR, got %+v", vs)
	}
	if vs := (&Config{RunDirErr: fmt.Errorf("AURA_RUN_DIR could not be resolved")}).gateRunDir(); !hasViolation(vs, "AURA_RUN_DIR", Fatal) {
		t.Errorf("RunDirErr must surface as Fatal naming AURA_RUN_DIR, got %+v", vs)
	}
	abs := filepath.Join(t.TempDir(), "runs")
	if vs := (&Config{RunDir: abs}).gateRunDir(); len(vs) != 0 {
		t.Errorf("absolute RunDir must pass, got %+v", vs)
	}
}

// TestValidateProfile proves the aggregator lists EVERY unmet requirement (never
// first-fail, criterion #1) and that Validate() is profile-aware (criterion #3/#4).
func TestValidateProfile(t *testing.T) {
	// criterion #1: an unsafe server_production config yields >=6 Fatal violations,
	// each naming its offending AURA_* knob in the joined Validate() output.
	t.Run("server_production aggregates >=6 fatals", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", "off")
		cfg := &Config{
			DB:                           db.Config{URL: "postgres://u:p@h:5432/db"},
			Profile:                      ProfileServerProduction,
			AGUIBind:                     "127.0.0.1:9080",
			RunDir:                       filepath.Join(t.TempDir(), "runs"),
			ObjectStoreAccessKey:         defaultObjectStoreAccessKey,
			ObjectStoreSecretKey:         defaultObjectStoreSecretKey,
			GarageRPCSecret:              "",
			ObjectStoreReplicationFactor: 1,
			AuthulaSecret:                "",
		}
		var fatals []Violation
		for _, v := range cfg.ValidateProfile(ProfileServerProduction) {
			if v.Sev == Fatal {
				fatals = append(fatals, v)
			}
		}
		if len(fatals) < 6 {
			t.Fatalf("unsafe server_production config must yield >=6 Fatal violations, got %d: %+v", len(fatals), fatals)
		}
		t.Logf("server_production Fatal count = %d", len(fatals))

		wantKnobs := []string{
			"AURA_OBJECTSTORE_ACCESS_KEY", "AURA_OBJECTSTORE_SECRET_KEY", "GARAGE_RPC_SECRET",
			"AURA_OBJECTSTORE_REPLICATION_FACTOR",
			"AURA_AUTHULA_SECRET", "AURA_SHELL_DESTRUCTIVE_PATTERNS",
		}
		for _, k := range wantKnobs {
			if !hasViolation(fatals, k, Fatal) {
				t.Errorf("missing Fatal violation for %s; fatals=%+v", k, fatals)
			}
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("Validate() must be non-nil for the unsafe prod config")
		}
		for _, k := range wantKnobs {
			if !strings.Contains(err.Error(), k) {
				t.Errorf("Validate() joined output must name %s, got %q", k, err.Error())
			}
		}
	})

	// criterion #3/#4: the SAME invalid int knob is a Warn under dev (Validate nil)
	// and a Fatal under server_production (Validate non-nil naming the knob).
	t.Run("dev tolerates invalid int (Warn, Validate nil)", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_SWARM_MAX_GOALS", "notanint")
		dev := &Config{
			DB:                           db.Config{URL: "postgres://u:p@h:5432/db"},
			Profile:                      ProfileDev,
			AGUIBind:                     "127.0.0.1:9080",
			RunDir:                       filepath.Join(t.TempDir(), "runs"),
			ObjectStoreReplicationFactor: 1,
		}
		if err := dev.Validate(); err != nil {
			t.Fatalf("invalid int under dev must be Warn (Validate nil), got %v", err)
		}
	})

	t.Run("server_production rejects invalid int (Fatal, Validate non-nil)", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_SWARM_MAX_GOALS", "notanint")
		prod := &Config{
			DB:                           db.Config{URL: "postgres://u:p@h:5432/db"},
			Profile:                      ProfileServerProduction,
			AGUIBind:                     "127.0.0.1:9080",
			RunDir:                       filepath.Join(t.TempDir(), "runs"),
			ObjectStoreAccessKey:         "GKrealoperatorkey0000000001",
			ObjectStoreSecretKey:         "realoperatorsecretvalue000000000000000000000000000000000000000000",
			GarageRPCSecret:              "deadbeefrpcsecret",
			ObjectStoreReplicationFactor: 3,
			AuthulaSecret:                "32byteshexsecretvalueforauthula0",
			ObjectStoreBucket:            "prod-assets",
			ObjectStoreEndpoint:          "https://garage.prod:3900",
		}
		err := prod.Validate()
		if err == nil {
			t.Fatal("invalid int under server_production must be Fatal (Validate non-nil)")
		}
		if !strings.Contains(err.Error(), "AURA_SWARM_MAX_GOALS") {
			t.Errorf("Validate() error must name AURA_SWARM_MAX_GOALS, got %q", err.Error())
		}
	})
}
