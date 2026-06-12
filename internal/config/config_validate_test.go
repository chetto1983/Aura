package config

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
)

// TestConfigValidate covers O-04: required infra secrets are checked at boot so a
// misconfigured deploy fails fast with a named error instead of a late cryptic
// auth failure (DB) or a silently degraded graph (empty NEO4J_PASSWORD).
func TestConfigValidate(t *testing.T) {
	full := func() *Config {
		return &Config{
			DB:    db.Config{URL: "postgres://u:p@h:5432/db"},
			Neo4j: knowledge.Config{Password: "neo-secret"},
		}
	}
	if err := full().Validate(); err != nil {
		t.Fatalf("fully-configured Config must validate, got %v", err)
	}

	c := full()
	c.Neo4j.Password = ""
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "NEO4J_PASSWORD") {
		t.Fatalf("empty NEO4J_PASSWORD must fail validation naming the var, got %v", err)
	}

	c2 := full()
	c2.DB.URL = ""
	if err := c2.Validate(); err == nil {
		t.Fatal("empty DB URL (no POSTGRES_PASSWORD / AURA_DB_URL) must fail validation")
	}
}
