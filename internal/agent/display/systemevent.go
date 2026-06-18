package display

import "github.com/chetto1983/aura/internal/web"

// severity classifies a web error code for the card's render hint. The web-safety
// enum (errors.go:11-20) is split into "warning" for a soft/recoverable downgrade
// and "error" for a hard block — neither carries any sensitive field.
var severityByCode = map[string]string{
	web.CodeSearchUnavailable:  "warning",
	web.CodeBlockedURL:         "error",
	web.CodeUnsupportedScheme:  "warning",
	web.CodeUnsupportedContent: "warning",
	web.CodeResponseTooLarge:   "warning",
	web.CodeTimeout:            "warning",
	web.CodeHTTPError:          "error",
	web.CodeExtractionFailed:   "warning",
}

// normalizeWebError maps a web.WebError to a system_event Payload (DISP-04 / D-07).
// CRITICAL: WebError is already the model-visible, sanitized shape (errors.go:32-41,
// produced by sanitize() :134) — it carries no resolved IP, internal host, or
// redirect chain. This normalizer copies ONLY its Code/Reason/Message fields, so no
// SSRF internal can ever reach a user-visible card. There is ZERO new classification
// here: the System fields are a 1:1 projection of the already-classified WebError.
func normalizeWebError(toolCallID string, we *web.WebError) (Payload, bool) {
	if we == nil {
		return Payload{}, false
	}
	return Payload{
		Type:       KindSystemEvent,
		ToolCallID: toolCallID,
		System: &System{
			Class:    we.Code,
			Reason:   we.Reason,
			Message:  we.Message,
			Severity: severityFor(we.Code),
		},
	}, true
}

// normalizeSwarmStatus maps a failed/needs-input swarm child Status to a
// system_event Payload (D-07). It consumes ONLY the classified Status enum, NEVER
// the free-form ChildReport.Error text (which could carry a leaky message): a
// failed child renders "swarm child failed", a needs-input child "swarm child
// awaiting input". A StatusOK child is not a system event (returns false).
func normalizeSwarmStatus(toolCallID, childID, status string) (Payload, bool) {
	var msg, severity string
	switch status {
	case StatusFailed:
		msg, severity = "swarm child failed", "error"
	case StatusNeedsUserInput:
		msg, severity = "swarm child awaiting input", "warning"
	default:
		return Payload{}, false
	}
	return Payload{
		Type:       KindSystemEvent,
		ToolCallID: toolCallID,
		System: &System{
			Class:    status,
			Reason:   childID,
			Message:  msg,
			Severity: severity,
		},
	}, true
}

// severityFor returns the render-hint severity for a web error code, defaulting to
// "error" for an unmapped code (fail safe — an unknown class is treated as hard).
func severityFor(code string) string {
	if s, ok := severityByCode[code]; ok {
		return s
	}
	return "error"
}
