package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
)

const (
	// DoclingImageV1290 is the sole production image admitted by Amendment #114.
	DoclingImageV1290 = "quay.io/docling-project/docling-serve-cpu:v1.29.0@sha256:51c18e0e0fec8a13ab858f03d5e9a140178d8d95962400553cb2644ad5ee84da"
	// DoclingTokenizer is the sole hybrid-chunk tokenizer family admitted by Amendment #114.
	DoclingTokenizer = "google/embeddinggemma-300m"

	// DefaultAssetMaxDocumentBytes is the shared 100 MiB ingress and Docling ceiling.
	DefaultAssetMaxDocumentBytes = 100 << 20
	defaultDocumentConvertSec    = 900
	defaultDocumentMaxPages      = 2000

	// defaultDocumentChunkTokens is the chunk budget handed to Docling's hybrid chunker.
	//
	// It was 512, and 512 was ALSO the validation ceiling below, so the knob could never be
	// raised — the default was the maximum. That cost recall, measured: Docling warns
	// "Chunks prefix is too long (637/709/729/1603 tokens) for chunk size 512. It will be
	// split into multiple chunks and only included in the first chunk(s)". The prefix is the
	// heading path that says WHICH section a passage belongs to, so on any document with real
	// structure most chunks were stored without the context that identifies them.
	defaultDocumentChunkTokens = 2048

	// maxDocumentChunkTokens is a physical limit, not a preference: an embedding needs the
	// whole sequence in ONE llama.cpp micro-batch, and the sidecar runs with -ub 4096
	// (compose.yaml, aura-llama-embed). A chunk above that is rejected by the sidecar rather
	// than truncated, which dead-lettered the embed stage the last time it happened.
	maxDocumentChunkTokens = 4096
)

// DocumentConfig is the immutable Docling conversion/chunking boundary from
// Amendment #114. Retrieval, projection and lifecycle knobs live with their owners.
type DocumentConfig struct {
	DoclingBaseURL  string
	DoclingAPIKey   string
	DoclingImage    string
	ConvertTimeout  time.Duration
	MaxRequestBytes int64
	MaxPages        int
	ChunkMaxTokens  int
	ChunkTokenizer  string

	// OCREnabled asks Docling to rasterise and read bitmap content. It is OFF by default,
	// which is a capability trade made on measured evidence rather than a preference.
	//
	// The bundled engines cannot read this deployment's corpus: RapidOCR (what `auto`
	// selects here) is Chinese/English-oriented, the image's tesseract carries only eng+osd,
	// and Docling's ocr_lang is never set. An Italian scan therefore OCRs to nothing —
	// observed as "RapidOCR returned empty result!" — while still costing minutes of CPU on
	// a 2-core docling-serve-cpu, which is long enough for a worker restart to land
	// mid-convert. Paying that for no text is worse than not paying it: a digital PDF still
	// yields its text layer without OCR.
	//
	// Re-enable per deployment with AURA_DOCUMENT_OCR_ENABLED=true once an engine that reads
	// the corpus is in place — the intended one is Docling's VLM pipeline against the
	// GLM-OCR sidecar. The flag travels into the conversion profile (and so into the stage
	// fingerprint), so flipping it re-converts rather than reusing text produced under the
	// other setting.
	OCREnabled bool
}

func loadDocumentConfig() DocumentConfig {
	return DocumentConfig{
		DoclingBaseURL: envDefault("AURA_DOCLING_BASE_URL", "http://127.0.0.1:5001"),
		DoclingAPIKey:  strings.TrimSpace(envDefault("AURA_DOCLING_API_KEY", "")),
		DoclingImage:   envDefault("AURA_DOCLING_IMAGE", DoclingImageV1290),
		OCREnabled:     envutil.BoolDefault("AURA_DOCUMENT_OCR_ENABLED", false),
		ConvertTimeout: time.Duration(envutil.IntDefault("AURA_DOCUMENT_CONVERT_TIMEOUT_SEC", defaultDocumentConvertSec)) * time.Second,
		MaxRequestBytes: int64(envutil.IntDefault(
			"AURA_ASSET_MAX_DOCUMENT_BYTES", DefaultAssetMaxDocumentBytes,
		)),
		MaxPages:       envutil.IntDefault("AURA_DOCUMENT_MAX_PAGES", defaultDocumentMaxPages),
		ChunkMaxTokens: envutil.IntDefault("AURA_DOCUMENT_CHUNK_MAX_TOKENS", defaultDocumentChunkTokens),
		ChunkTokenizer: envDefault("AURA_DOCUMENT_CHUNK_TOKENIZER", DoclingTokenizer),
	}
}

func (c DocumentConfig) isZero() bool {
	return c == (DocumentConfig{})
}

// Validate rejects a boundary that could exfiltrate document bytes, exceed the
// declared appliance envelope, or silently select mutable deployment artifacts.
func (c DocumentConfig) Validate() error {
	if c.isZero() {
		return nil
	}
	if strings.TrimSpace(c.DoclingAPIKey) == "" {
		return fmt.Errorf("AURA_DOCLING_API_KEY is required")
	}
	if err := validatePrivateDoclingURL(c.DoclingBaseURL); err != nil {
		return fmt.Errorf("AURA_DOCLING_BASE_URL: %w", err)
	}
	if c.DoclingImage != DoclingImageV1290 {
		return fmt.Errorf("AURA_DOCLING_IMAGE must equal the v1.29.0 digest-pinned image")
	}
	if c.ConvertTimeout <= 0 || c.ConvertTimeout > defaultDocumentConvertSec*time.Second {
		return fmt.Errorf("AURA_DOCUMENT_CONVERT_TIMEOUT_SEC must be between 1 and %d", defaultDocumentConvertSec)
	}
	if c.MaxRequestBytes <= 0 || c.MaxRequestBytes > DefaultAssetMaxDocumentBytes {
		return fmt.Errorf("AURA_ASSET_MAX_DOCUMENT_BYTES must be between 1 and %d", DefaultAssetMaxDocumentBytes)
	}
	if c.MaxPages <= 0 || c.MaxPages > defaultDocumentMaxPages {
		return fmt.Errorf("AURA_DOCUMENT_MAX_PAGES must be between 1 and %d", defaultDocumentMaxPages)
	}
	if c.ChunkMaxTokens <= 0 || c.ChunkMaxTokens > maxDocumentChunkTokens {
		return fmt.Errorf("AURA_DOCUMENT_CHUNK_MAX_TOKENS must be between 1 and %d", maxDocumentChunkTokens)
	}
	if c.ChunkTokenizer != DoclingTokenizer {
		return fmt.Errorf("AURA_DOCUMENT_CHUNK_TOKENIZER must equal the pinned tokenizer family")
	}
	return nil
}

func validatePrivateDoclingURL(raw string) error {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("credentials, query, fragment and path are forbidden")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if port := u.Port(); port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return fmt.Errorf("port is invalid")
		}
	}
	if host == "localhost" {
		return nil
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		ip = ip.Unmap()
		if ip.IsLoopback() || ip.IsPrivate() {
			return nil
		}
		return fmt.Errorf("host must be loopback or private")
	}
	if strings.Contains(host, ".") || !validPrivateDNSLabel(host) {
		return fmt.Errorf("DNS host must be a private single-label service name")
	}
	return nil
}

func validPrivateDNSLabel(host string) bool {
	if len(host) == 0 || len(host) > 63 || host[0] == '-' || host[len(host)-1] == '-' {
		return false
	}
	for _, r := range host {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func (c *Config) gateDocument(p RuntimeProfile) []Violation {
	if c == nil || c.Document.isZero() {
		return nil
	}
	document := c.Document
	var violations []Violation
	if strings.TrimSpace(document.DoclingAPIKey) == "" {
		severity := Warn
		if p.Strict() {
			severity = Fatal
		}
		violations = append(violations, Violation{
			Knob: "AURA_DOCLING_API_KEY", Sev: severity,
			Msg: "required before the authenticated Docling boundary is enabled",
		})
		document.DoclingAPIKey = "validation-placeholder"
	}
	if err := document.Validate(); err != nil {
		return []Violation{{Knob: "AURA_DOCUMENT_*", Sev: Fatal, Msg: err.Error()}}
	}
	return violations
}
