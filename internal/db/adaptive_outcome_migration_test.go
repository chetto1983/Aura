package db

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestAdaptiveOutcomeMigration0066SourceIsImmutable(t *testing.T) {
	for _, migration := range []struct {
		name string
		hash string
	}{
		{
			name: "0066_adaptive_typed_outcomes.up.sql",
			hash: "4fbd779740efe909fa7d48f2737119c10b4c35ef7d430718a1afd4229ccb314f",
		},
		{
			name: "0066_adaptive_typed_outcomes.down.sql",
			hash: "4b03ac6a181d387719bad65fcca71cdd76f6fd073c246f9bff07d7ba9dbf51d7",
		},
	} {
		body, err := migrationsFS.ReadFile("migrations/" + migration.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != migration.hash {
			t.Fatalf("%s changed after release: sha256=%s", migration.name, got)
		}
	}
}

func TestAdaptiveOutcomeMigration0067OnlyReplacesChainLimit(t *testing.T) {
	v66Bytes, err := migrationsFS.ReadFile(
		"migrations/0066_adaptive_typed_outcomes.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	v66 := string(v66Bytes)
	start := strings.Index(
		v66,
		"CREATE FUNCTION aura.enforce_adaptive_schema2_outcome_chain()",
	)
	if start < 0 {
		t.Fatal("cannot find the released 0066 outcome-chain function")
	}
	end := strings.Index(v66[start:], "\n\nCREATE TRIGGER ")
	if end < 0 {
		t.Fatal("cannot find the end of the released 0066 outcome-chain function")
	}
	v66Function := v66[start:start+end] + "\n"
	wantDown := strings.Replace(
		v66Function,
		"CREATE FUNCTION",
		"CREATE OR REPLACE FUNCTION",
		1,
	)
	wantUp := strings.Replace(
		wantDown,
		"IF chain_depth > 64 THEN",
		"IF chain_depth >= 64 THEN",
		1,
	)
	if wantUp == wantDown {
		t.Fatal("0067 test did not change the correction-chain boundary")
	}

	upBytes, err := migrationsFS.ReadFile(
		"migrations/0067_adaptive_correction_chain_limit.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(upBytes); got != wantUp {
		t.Error("0067 up must replace only the 0066 function and tighten its chain limit")
	}
	downBytes, err := migrationsFS.ReadFile(
		"migrations/0067_adaptive_correction_chain_limit.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(downBytes); got != wantDown {
		t.Error("0067 down must restore the exact 0066 function behavior")
	}
}
