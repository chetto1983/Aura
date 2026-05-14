// Package markitdown wraps Microsoft's markitdown-mcp sidecar so Aura's
// source pipeline has a single extractor for office, ebook, html, zip, and
// (in 2.9.5) image formats.
//
// The sidecar speaks MCP JSON-RPC 2.0 over Streamable HTTP and exposes one
// tool: convert_to_markdown(uri). For binary uploads we send a
// data:<mime>;base64,<...> URI. URL ingest goes through web_fetch first so
// the SSRF gate stays at Aura's boundary; markitdown only ever sees bytes
// Aura already authorized.
package markitdown

import "context"

// ConvertInput is the per-call payload.
type ConvertInput struct {
	// Bytes is the raw file body. Required.
	Bytes []byte
	// MimeType drives markitdown's converter selection. Required; if the
	// caller has no better information, "application/octet-stream" is the
	// safe default and markitdown will sniff by magic bytes.
	MimeType string
	// Filename is a hint for converter heuristics (extension lookup). Optional.
	Filename string
}

// ConvertResult bundles markdown plus any warnings emitted by the sidecar.
type ConvertResult struct {
	Markdown string
	Warnings []string
}

// Converter is the contract used by the source/ingest pipeline. Production
// uses Client; tests use a fake.
type Converter interface {
	Convert(ctx context.Context, in ConvertInput) (ConvertResult, error)
}
