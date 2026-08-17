package mcp

import "strings"

// apps.go is Aura's reading of the MCP Apps extension (SEP-1865,
// `io.modelcontextprotocol/ui`): a server may bind a tool to a `ui://` resource
// — an HTML document a host renders in a sandboxed iframe — and the tool still
// returns its normal payload to a host that renders nothing.
//
// Everything here is SERVER-AGNOSTIC on purpose. A view is identified by the
// pair (mounted server name, resource URI) and nothing else; the framing rules
// come from the `_meta.ui` the declaring resource carries, never from a table in
// Aura that would need an entry per server. The first server to use this is a
// fork of ours, and that must not be visible anywhere in this file.
//
// This file is pure: it parses metadata and answers policy questions. Reading
// the resource, carrying it to a surface and framing it belong to their own
// layers.

const (
	// AppsExtensionID is the extension identifier a client declares in its
	// capabilities and a server declares back. Reverse-DNS per SEP-2133.
	AppsExtensionID = "io.modelcontextprotocol/ui"

	// AppMIMEType is the only MIME type a `ui://` resource is served as. A host
	// must not render such a resource under any other type.
	AppMIMEType = "text/html;profile=mcp-app"

	// AppURIScheme prefixes every view resource.
	AppURIScheme = "ui://"
)

// ViewRef is what a TOOL carries: which view renders its result. It is read from
// the tool's `_meta.ui`.
type ViewRef struct {
	// ResourceURI is the `ui://` document to render. Always non-empty on a ref
	// that parsed.
	ResourceURI string
	// Visibility is where the server means the tool to be surfaced ("model",
	// "app"). Empty means the server said nothing, which is not the same as
	// saying "model": a caller that cares must treat empty as its own default.
	Visibility []string
}

// ViewPolicy is what a RESOURCE declares about how it must be framed: the CSP
// domain lists, the iframe permissions it asks for, and two display hints. It is
// read from the resource's `_meta.ui`.
//
// A nil slice and an empty slice differ and the difference is load-bearing. Nil
// means the server declared nothing and the host applies its own restrictive
// default; empty means the server declared "no domains at all", which is a
// stronger statement the host can seal against. Distinguishing them is why these
// are pointers to slices nowhere and instead carry Declared* flags.
type ViewPolicy struct {
	ConnectDomains  []string
	ResourceDomains []string
	FrameDomains    []string
	BaseURIDomains  []string

	// DeclaredCSP is true when the resource carried a `csp` object at all, so a
	// host can tell "sealed" (declared, empty) from "unspecified" (absent).
	DeclaredCSP bool

	// Permissions are the iframe feature requests (camera, microphone,
	// geolocation, clipboardWrite). A host MAY honour them; it is never obliged
	// to, and Aura's framing layer decides that on its own.
	Permissions map[string]any

	// Domain is the server's optional grouping hint for related views.
	Domain string

	// PrefersBorder is the server asking to be drawn with a frame around it.
	PrefersBorder bool
}

// Sealed reports whether the resource declared a CSP that permits no domain in
// any direction — a view that says outright it fetches nothing. It is the
// tightest thing a server can declare and the cheapest for a host to enforce.
func (p ViewPolicy) Sealed() bool {
	return p.DeclaredCSP &&
		len(p.ConnectDomains) == 0 &&
		len(p.ResourceDomains) == 0 &&
		len(p.FrameDomains) == 0 &&
		len(p.BaseURIDomains) == 0
}

// AppsClientSettings is the settings object Aura sends when declaring the
// extension, naming the MIME types it can render. A server keys on this exact
// shape to decide whether a UI-bound tool should also produce rich text.
//
// Declaring it is a PROMISE: a server that sees it may trim the text it would
// otherwise return, on the understanding that the human sees the view instead.
// It must therefore be declared only once a surface actually renders views —
// announcing it earlier makes the server's text output worse for nothing.
func AppsClientSettings() map[string]any {
	return map[string]any{"mimeTypes": []any{AppMIMEType}}
}

// IsAppURI reports whether uri names a view resource.
func IsAppURI(uri string) bool {
	return strings.HasPrefix(uri, AppURIScheme) && len(uri) > len(AppURIScheme)
}

// ViewRefFromMeta reads a tool's `_meta.ui` reference. It returns ok=false for
// absent metadata, a non-object `ui` entry, a missing or blank `resourceUri`, or
// a `resourceUri` that is not a `ui://` URI — a tool pointing anywhere else is a
// misdeclaration, and resolving it would let a server aim the host at a scheme
// the sandbox was never designed for.
func ViewRefFromMeta(meta map[string]any) (ViewRef, bool) {
	ui, ok := meta[uiMetaKey].(map[string]any)
	if !ok {
		return ViewRef{}, false
	}
	uri := strings.TrimSpace(stringField(ui, "resourceUri"))
	if !IsAppURI(uri) {
		return ViewRef{}, false
	}
	return ViewRef{ResourceURI: uri, Visibility: stringsField(ui, "visibility")}, true
}

// ViewPolicyFromMeta reads a resource's `_meta.ui` framing declaration. Absent
// or malformed metadata yields the zero policy, which reads as "the server
// declared nothing" — the host then applies its own default rather than
// inventing a permissive one on the server's behalf.
func ViewPolicyFromMeta(meta map[string]any) ViewPolicy {
	ui, ok := meta[uiMetaKey].(map[string]any)
	if !ok {
		return ViewPolicy{}
	}
	policy := ViewPolicy{
		Domain:        strings.TrimSpace(stringField(ui, "domain")),
		PrefersBorder: boolField(ui, "prefersBorder"),
	}
	if permissions, ok := ui["permissions"].(map[string]any); ok {
		policy.Permissions = permissions
	}
	csp, ok := ui["csp"].(map[string]any)
	if !ok {
		return policy
	}
	policy.DeclaredCSP = true
	policy.ConnectDomains = stringsField(csp, "connectDomains")
	policy.ResourceDomains = stringsField(csp, "resourceDomains")
	policy.FrameDomains = stringsField(csp, "frameDomains")
	policy.BaseURIDomains = stringsField(csp, "baseUriDomains")
	return policy
}

// TrustMayRenderViews reports whether a server of this trust class is allowed to
// put its HTML in front of the operator.
//
// Rendering is a strictly larger grant than calling: a tool result is text Aura
// frames, while a view is the server's own document running in the operator's
// browser. So the classes Aura already treats as its own infrastructure render,
// and the rest do not:
//
//   - trusted_recipe, trusted_local — shipped or installed as infrastructure;
//   - sandboxed_local — deliberately confined, and a view is an escape from
//     exactly the confinement that class exists to impose;
//   - remote_http — third-party HTML, the case the sandbox proxy was designed
//     for but which Aura has no per-server consent surface for yet;
//   - blocked — never.
//
// A denied server is not broken by this: its tools still return their payload,
// which is the extension's own progressive-enhancement contract. When a
// per-server consent surface exists, remote_http becomes an operator decision
// and this function grows a grant argument rather than a special case.
func TrustMayRenderViews(trust string) bool {
	switch strings.TrimSpace(trust) {
	case TrustTrustedRecipe, TrustTrustedLocal:
		return true
	default:
		return false
	}
}

// uiMetaKey is the `_meta` member both tools and resources hang their Apps
// declaration off.
const uiMetaKey = "ui"

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

// stringsField reads a JSON array of strings, keeping nil distinct from empty:
// a missing key yields nil ("unspecified"), a present-but-empty array yields a
// non-nil empty slice ("declared as none"). Non-string members are dropped
// rather than failing the whole read — a server that adds a richer member later
// must not blind this host to the ones it understands.
func stringsField(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
