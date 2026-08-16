package display

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/web"
)

// TestNormalizeWebError (DISP-04): a sanitized WebError becomes a system_event whose
// fields are a 1:1 projection of the safe enum.
func TestNormalizeWebError(t *testing.T) {
	we := &web.WebError{Code: web.CodeBlockedURL, Reason: web.ReasonPrivateOrMetadata, Message: "blocked"}
	p, ok := normalizeWebError("call-1", we)
	if !ok || p.Type != KindSystemEvent {
		t.Fatalf("normalizeWebError = (%v, %v), want system_event/true", p.Type, ok)
	}
	if p.System == nil || p.System.Class != web.CodeBlockedURL || p.System.Reason != web.ReasonPrivateOrMetadata {
		t.Fatalf("system payload wrong: %+v", p.System)
	}
	if p.System.Severity != "error" {
		t.Fatalf("blocked_url severity = %q, want error", p.System.Severity)
	}
}

// TestNormalizeWebErrorNil: a nil WebError is not a system event (fallback).
func TestNormalizeWebErrorNil(t *testing.T) {
	if _, ok := normalizeWebError("c", nil); ok {
		t.Fatalf("nil WebError recognized as a system event")
	}
}

// TestSystemEventLeaksNoSSRFInternals is the DISP-04 / T-26-02 security assertion:
// the rendered system_event carries ONLY the sanitized enum — no raw internal URL,
// IP, or host reaches the user-visible card. WebError is the sanitize() output by
// construction (it has no sensitive field), so the normalizer cannot leak; this
// test pins that contract against every web-safety code.
func TestSystemEventLeaksNoSSRFInternals(t *testing.T) {
	// The sensitive strings sanitize() strips — none may appear in any System field.
	forbidden := []string{"169.254.169.254", "127.0.0.1", "10.0.0.5", "internal-host.local", "redirect=", "ip="}
	codes := []string{
		web.CodeSearchUnavailable, web.CodeBlockedURL, web.CodeUnsupportedScheme,
		web.CodeUnsupportedContent, web.CodeResponseTooLarge, web.CodeTimeout,
		web.CodeHTTPError, web.CodeExtractionFailed,
	}
	for _, code := range codes {
		// A model-visible WebError as the adapters produce it: only the safe reason.
		we := &web.WebError{Code: code, Reason: web.ReasonPrivateOrMetadata, Message: "request blocked"}
		p, ok := normalizeWebError("c", we)
		if !ok {
			t.Fatalf("code %q not recognized", code)
		}
		blob := p.System.Class + "|" + p.System.Reason + "|" + p.System.Message + "|" + p.System.Severity
		for _, bad := range forbidden {
			if strings.Contains(blob, bad) {
				t.Fatalf("code %q system_event leaked sensitive token %q: %q", code, bad, blob)
			}
		}
		if p.System.Severity == "" {
			t.Fatalf("code %q produced an empty severity", code)
		}
	}
}

// TestSeverityForUnknownCodeFailsSafe: an unmapped error code defaults to "error"
// (an unknown class is treated as a hard block, never silently a warning).
func TestSeverityForUnknownCodeFailsSafe(t *testing.T) {
	if got := severityFor("some_unmapped_code"); got != "error" {
		t.Fatalf("severityFor(unmapped) = %q, want error", got)
	}
}
