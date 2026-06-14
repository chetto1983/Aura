//go:build db_integration && neo4j_integration

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func TestDoctorLiveStack(t *testing.T) {
	if !doctorLiveEnvReady() {
		msg := "doctor live stack requires Postgres and Neo4j env (POSTGRES_PASSWORD or AURA_DB_URL, plus NEO4J_PASSWORD)"
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}

	var out bytes.Buffer
	code := runDoctorWithConfig(context.Background(), &out, config.LoadDB())
	if code != 0 {
		t.Fatalf("aura doctor live exit = %d, want 0:\n%s", code, out.String())
	}
	for _, want := range []string{"PASS postgres:", "PASS neo4j:", "PASS embed:", "PASS mcp-neo4j-cypher:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("doctor live output missing %q:\n%s", want, out.String())
		}
	}
}

func doctorLiveEnvReady() bool {
	hasPostgres := os.Getenv("AURA_DB_URL") != "" || os.Getenv("POSTGRES_PASSWORD") != ""
	return hasPostgres && os.Getenv("NEO4J_PASSWORD") != ""
}
