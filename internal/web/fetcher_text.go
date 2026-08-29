package web

import (
	"strings"
)

// fetcher_text.go is the NON-HTML lane of web_fetch: the response types that carry
// text a model can read directly but that readability has nothing to do with — a JSON
// API payload, a CSV, a plain-text file.
//
// Why the lane exists at all (measured 2026-08-29 on the live stack): the allowlist
// admitted only text/html + application/xhtml+xml, so `web_fetch` on
// api.open-meteo.com answered unsupported_content_type and the agent fell back to
// `shell_exec curl`. That fallback is the actual security regression — curl inside the
// box bypasses the SSRF guard, the size cap and the fetch audit trail, all of which
// this package enforces. Refusing the type did not keep the traffic out; it moved the
// traffic somewhere with no controls.
//
// What the allowlist was NOT: it was never the SSRF or DoS defense. Those run earlier
// and are content-type agnostic — ssrf.go classifies every resolved IP (loopback,
// private, link-local) before the dial, and gateAndRead's io.LimitReader caps the body
// before it is read. Both apply to this lane unchanged.
//
// Prompt injection is not a reason to keep JSON out either: untrusted text reaches the
// model through the HTML lane already, and HTML is the WORSE carrier (hidden nodes,
// off-screen text, an accessibility tree the model reads and a human never sees). The
// fence below is the mitigation that actually helps — it marks where third-party bytes
// begin and end, rather than letting them blend into the surrounding instructions.

// allowedTextContentTypes is the readable non-HTML allowlist. Every entry is a type
// whose bytes ARE the content, so serving them verbatim is the honest rendering; a
// type needing a decoder (pdf, docx, images) is deliberately absent — that is the
// document pipeline's job, not this one's.
var allowedTextContentTypes = map[string]struct{}{
	"application/json":          {},
	"application/xml":           {},
	"application/x-ndjson":      {},
	"application/yaml":          {},
	"text/plain":                {},
	"text/csv":                  {},
	"text/markdown":             {},
	"text/tab-separated-values": {},
	"text/xml":                  {},
	"text/yaml":                 {},
}

// textStructuredSuffixes are the RFC 6839 structured syntax suffixes that promise a
// known underlying representation. Matching them is what lets application/vnd.api+json
// and application/ld+json through: a receiver may process the underlying syntax
// generically WITHOUT understanding the vendor semantics, which is precisely the
// guarantee the suffix exists to give (RFC 6839 §1). Without this, every vendor JSON
// API — a large share of the real ones — would still be refused.
var textStructuredSuffixes = []string{"+json", "+xml", "+yaml"}

// suffixTopLevelTypes are the top-level types a structured suffix may promote into the
// text lane. The restriction is not decoration: image/svg+xml carries the +xml suffix,
// and a bare suffix rule admits it — which would contradict fetcher_image.go, where SVG
// is excluded BY NAME because it can carry inline <script>. A resource whose top-level
// type says image, audio, video or font is not readable text whatever syntax it is
// serialised in, so the suffix never speaks for it.
var suffixTopLevelTypes = []string{"application/", "text/"}

// fenceLang maps a media type to the info string of the code fence it is wrapped in,
// so the model is told what it is reading instead of inferring it from the bytes.
var fenceLang = map[string]string{
	"application/json":     "json",
	"application/x-ndjson": "jsonl",
	"application/xml":      "xml",
	"application/yaml":     "yaml",
	"text/csv":             "csv",
	"text/xml":             "xml",
	"text/yaml":            "yaml",
}

// contentKind is which lane a gated response takes.
type contentKind int

const (
	kindUnsupported contentKind = iota
	kindHTML
	kindText
)

// classifyContentType maps a Content-Type header to its lane. The media type is
// matched alone (charset and other params stripped), as contentTypeAllowed always
// did. An empty header is unsupported rather than guessed: sniffing the bytes would
// let a server pick our lane for us, and the whole point of the header check is that
// the decision is ours.
func classifyContentType(ct string) (media string, kind contentKind) {
	media = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if _, ok := allowedContentTypes[media]; ok {
		return media, kindHTML
	}
	if _, ok := allowedTextContentTypes[media]; ok {
		return media, kindText
	}
	if hasSuffixTopLevelType(media) {
		for _, suffix := range textStructuredSuffixes {
			if strings.HasSuffix(media, suffix) {
				return media, kindText
			}
		}
	}
	return media, kindUnsupported
}

// hasSuffixTopLevelType reports whether media's top-level type is one a structured
// syntax suffix may speak for.
func hasSuffixTopLevelType(media string) bool {
	for _, prefix := range suffixTopLevelTypes {
		if strings.HasPrefix(media, prefix) {
			return true
		}
	}
	return false
}

// renderText wraps a non-HTML body in a fenced block labelled with its media type.
//
// The bytes are NOT reformatted — no JSON re-indent, no pretty-print. That matches
// what the MCP fetch server and Anthropic's own web_fetch do (raw content, labelled),
// and it avoids a transformation that can fail on the payload it is meant to help
// with. Truncation is likewise absent BY DESIGN: tools.NewResult already caps the
// tool result, spills the remainder to a sidecar and appends the read_tool_output
// footer the model uses to page through it, so a second truncation here would cut the
// content before the mechanism that knows how to hand back the rest ever saw it.
//
// The fence is grown past any run of backticks in the body, per the CommonMark rule
// that a fenced block ends only on a run at least as long as its opener — a payload
// containing ``` must not be able to close its own fence and spill into the
// surrounding prose as if it were instructions.
func renderText(body []byte, media string) (markdown, warning string) {
	content := strings.ToValidUTF8(string(body), "�")
	fence := strings.Repeat("`", maxBacktickRun(content)+1)
	return fence + fenceLang[media] + "\n" + content + "\n" + fence, WarningRawContent
}

// maxBacktickRun returns the longest run of consecutive backticks in s, floored at 2
// so the result is always a valid fence of at least three.
func maxBacktickRun(s string) int {
	longest, current := 2, 0
	for _, r := range s {
		if r != '`' {
			current = 0
			continue
		}
		current++
		if current > longest {
			longest = current
		}
	}
	return longest
}
