package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDocumentConfigDefaultsAndOverrides(t *testing.T) {
	t.Setenv("AURA_DOCLING_API_KEY", "test-key")
	cfg := loadDocumentConfig()
	if cfg.DoclingBaseURL != "http://127.0.0.1:5001" || cfg.DoclingImage != DoclingImageV1290 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ConvertTimeout != 15*time.Minute || cfg.MaxRequestBytes != 100<<20 ||
		cfg.MaxPages != 2000 || cfg.ChunkMaxTokens != 512 || cfg.ChunkTokenizer != DoclingTokenizer {
		t.Fatalf("unexpected limits: %+v", cfg)
	}

	t.Setenv("AURA_DOCLING_BASE_URL", "http://aura-docling:5001")
	t.Setenv("AURA_DOCUMENT_CONVERT_TIMEOUT_SEC", "30")
	t.Setenv("AURA_ASSET_MAX_DOCUMENT_BYTES", "4096")
	t.Setenv("AURA_DOCUMENT_MAX_PAGES", "12")
	t.Setenv("AURA_DOCUMENT_CHUNK_MAX_TOKENS", "128")
	cfg = loadDocumentConfig()
	if cfg.DoclingBaseURL != "http://aura-docling:5001" || cfg.ConvertTimeout != 30*time.Second ||
		cfg.MaxRequestBytes != 4096 || cfg.MaxPages != 12 || cfg.ChunkMaxTokens != 128 {
		t.Fatalf("overrides not honored: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestDocumentConfigValidation(t *testing.T) {
	valid := DocumentConfig{
		DoclingBaseURL:  "http://127.0.0.1:5001",
		DoclingAPIKey:   "secret",
		DoclingImage:    DoclingImageV1290,
		ConvertTimeout:  time.Minute,
		MaxRequestBytes: 1024,
		MaxPages:        10,
		ChunkMaxTokens:  128,
		ChunkTokenizer:  DoclingTokenizer,
	}
	tests := []struct {
		name string
		edit func(*DocumentConfig)
		want string
	}{
		{name: "missing key", edit: func(c *DocumentConfig) { c.DoclingAPIKey = "" }, want: "AURA_DOCLING_API_KEY"},
		{name: "public URL", edit: func(c *DocumentConfig) { c.DoclingBaseURL = "https://docling.example.com" }, want: "AURA_DOCLING_BASE_URL"},
		{name: "URL credentials", edit: func(c *DocumentConfig) { c.DoclingBaseURL = "http://u:p@127.0.0.1:5001" }, want: "AURA_DOCLING_BASE_URL"},
		{name: "URL path", edit: func(c *DocumentConfig) { c.DoclingBaseURL = "http://127.0.0.1:5001/v1" }, want: "AURA_DOCLING_BASE_URL"},
		{name: "mutable image", edit: func(c *DocumentConfig) { c.DoclingImage = "quay.io/docling-project/docling-serve-cpu:latest" }, want: "AURA_DOCLING_IMAGE"},
		{name: "timeout", edit: func(c *DocumentConfig) { c.ConvertTimeout = 16 * time.Minute }, want: "AURA_DOCUMENT_CONVERT_TIMEOUT_SEC"},
		{name: "request bytes", edit: func(c *DocumentConfig) { c.MaxRequestBytes = 0 }, want: "AURA_ASSET_MAX_DOCUMENT_BYTES"},
		{name: "pages", edit: func(c *DocumentConfig) { c.MaxPages = 2001 }, want: "AURA_DOCUMENT_MAX_PAGES"},
		{name: "tokens", edit: func(c *DocumentConfig) { c.ChunkMaxTokens = 513 }, want: "AURA_DOCUMENT_CHUNK_MAX_TOKENS"},
		{name: "tokenizer", edit: func(c *DocumentConfig) { c.ChunkTokenizer = "other/model" }, want: "AURA_DOCUMENT_CHUNK_TOKENIZER"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.edit(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := (DocumentConfig{}).Validate(); err != nil {
		t.Fatalf("zero programmatic config rejected: %v", err)
	}
}

func TestValidatePrivateDoclingURL(t *testing.T) {
	accepted := []string{
		"http://localhost:5001", "http://127.0.0.1:5001", "http://10.0.0.8:5001",
		"http://172.16.2.4:5001", "http://192.168.1.9:5001", "http://aura-docling:5001",
	}
	for _, raw := range accepted {
		if err := validatePrivateDoclingURL(raw); err != nil {
			t.Errorf("%q rejected: %v", raw, err)
		}
	}
	rejected := []string{
		"", "ftp://127.0.0.1:5001", "http://8.8.8.8:5001", "https://docling.example.com",
		"http://127.0.0.1:0", "http://127.0.0.1:5001/path", "http://127.0.0.1:5001?q=x",
		"http://bad_host:5001",
	}
	for _, raw := range rejected {
		if err := validatePrivateDoclingURL(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestGateDocumentDoesNotEchoSecret(t *testing.T) {
	cfg := &Config{Document: DocumentConfig{DoclingBaseURL: "https://public.example", DoclingAPIKey: "do-not-print"}}
	violations := cfg.gateDocument(ProfileDev)
	if len(violations) != 1 || strings.Contains(violations[0].Msg, "do-not-print") {
		t.Fatalf("violations = %+v", violations)
	}
	if violations[0].Sev != Fatal {
		t.Fatalf("severity = %v, want Fatal", violations[0].Sev)
	}
}

func TestGateDocumentAPIKeyPolicyByProfile(t *testing.T) {
	document := loadDocumentConfig()
	document.DoclingAPIKey = ""
	cfg := &Config{Document: document}

	dev := cfg.gateDocument(ProfileDev)
	if len(dev) != 1 || dev[0].Knob != "AURA_DOCLING_API_KEY" || dev[0].Sev != Warn {
		t.Fatalf("dev violations = %+v", dev)
	}
	for _, violation := range dev {
		if violation.Sev == Fatal {
			t.Fatalf("dev must remain non-fatal: %+v", dev)
		}
	}

	production := cfg.gateDocument(ProfileServerProduction)
	if len(production) != 1 || production[0].Knob != "AURA_DOCLING_API_KEY" || production[0].Sev != Fatal {
		t.Fatalf("production violations = %+v", production)
	}
}
