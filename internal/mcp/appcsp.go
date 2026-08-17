package mcp

import (
	"regexp"
	"strings"
)

// appcsp.go turns a server's `_meta.ui.csp` declaration into the actual Content-
// Security-Policy the view document is served under, and arms the document with
// it before it ever reaches a browser.
//
// The policy is composed HERE, in Go, rather than in the cockpit, for one reason:
// the declared domains are strings a mounted server chose, and they end up inside
// a CSP. A server that could write `foo.test; script-src *` into one would turn
// its own declaration into a policy bypass. The validator below is what stops
// that, and it belongs next to unit tests rather than in a browser bundle.
//
// The shape of the policy is fixed and the declaration can only fill in the
// directives the extension defines (SEP-1865: connectDomains, resourceDomains,
// frameDomains, baseUriDomains). A server cannot introduce a directive, only
// populate one — so the worst a hostile declaration achieves is naming a host in
// a slot the spec already allows it to name.

// viewOrigin admits an https origin, optionally with a leading-label wildcard and
// an explicit port, and nothing else. No scheme other than https, no path, no
// query, no comma, no semicolon, no whitespace — the four characters that would
// let a declaration escape its directive.
var viewOrigin = regexp.MustCompile(`^https://(\*\.)?[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*(:[0-9]{1,5})?$`)

// ContentSecurityPolicy renders the policy a view document is served under.
//
// The floor is total: nothing loads, nothing connects, no form submits, no base
// tag redirects a relative URL. On top of that sit the four directives the
// extension lets a server populate, plus the two grants every view needs to be a
// document at all — its own inline script and inline style, which is what a
// single self-contained HTML resource IS.
//
// data: and blob: are allowed for images, fonts and media because a sealed view
// carries its assets inline; they reach nothing off the machine.
func (p ViewPolicy) ContentSecurityPolicy() string {
	resources := validOrigins(p.ResourceDomains)
	directives := []string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"style-src " + join("'unsafe-inline'", resources),
		"img-src " + join("data: blob:", resources),
		"font-src " + join("data:", resources),
		"media-src " + join("data: blob:", resources),
		"connect-src " + orNone(validOrigins(p.ConnectDomains)),
		"frame-src " + orNone(validOrigins(p.FrameDomains)),
		"base-uri " + orNone(validOrigins(p.BaseURIDomains)),
		"form-action 'none'",
	}
	return strings.Join(directives, "; ")
}

// WithoutHost drops every declared domain that resolves to hostname, whatever
// port or wildcard it wears.
//
// This closes the hole cookies open. A cookie is scoped by host and IGNORES the
// port, so a browser sends the operator's session to https://aura.lan:8444 just
// as readily as to https://aura.lan — meaning a view that got `connect-src
// https://aura.lan` in its policy could call Aura's own API AS the operator from
// inside the sandbox. The sandbox origin is the wall; a declared domain pointing
// back through it is a door in that wall, so the host takes it out.
//
// A deployment that gives the sandbox a hostname of its own does not need this.
// One that gives it a port does, and that is the deployment the appliance ships.
func (p ViewPolicy) WithoutHost(hostname string) ViewPolicy {
	if strings.TrimSpace(hostname) == "" {
		return p
	}
	p.ConnectDomains = dropHost(p.ConnectDomains, hostname)
	p.ResourceDomains = dropHost(p.ResourceDomains, hostname)
	p.FrameDomains = dropHost(p.FrameDomains, hostname)
	p.BaseURIDomains = dropHost(p.BaseURIDomains, hostname)
	return p
}

func dropHost(domains []string, hostname string) []string {
	if domains == nil {
		return nil
	}
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		if !sameHost(domain, hostname) {
			out = append(out, domain)
		}
	}
	return out
}

// sameHost compares a declared source against a hostname, honouring the one
// wildcard form CSP allows.
//
// It errs toward dropping: `https://*.example.com` does not match example.com in
// CSP's own matching rules, but a wildcard over the deployment's own domain is
// dropped anyway. Over-dropping costs a view a host it asked for; under-dropping
// costs the operator their session.
func sameHost(source, hostname string) bool {
	host := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(source), "https://"), "http://"))
	if idx := strings.IndexByte(host, ':'); idx >= 0 {
		host = host[:idx]
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if suffix, ok := strings.CutPrefix(host, "*."); ok {
		return hostname == suffix || strings.HasSuffix(hostname, "."+suffix)
	}
	return host == hostname
}

// ArmedHTML returns the view document with its policy attached as the first thing
// in <head>, which is where a meta CSP has to be to govern what follows it.
//
// A meta tag rather than a response header because the document is not served as
// a document: it travels to the cockpit inside a JSON field and is rendered from
// a srcdoc frame, where no header of ours would follow it. The tag travels with
// the bytes, so the policy cannot be lost in transit.
//
// A document with no <head> gets one. A document that already carries its own
// CSP meta keeps it AND gets ours — two policies both apply, and the browser
// enforces the intersection, so the server cannot loosen what Aura set.
func ArmedHTML(html, csp string) string {
	meta := `<meta http-equiv="Content-Security-Policy" content="` + strings.ReplaceAll(csp, `"`, `&quot;`) + `">`
	if idx := headInsertionPoint(html); idx >= 0 {
		return html[:idx] + meta + html[idx:]
	}
	return meta + html
}

// headInsertionPoint locates the byte just after the opening <head> tag, or -1
// when the document has none.
func headInsertionPoint(html string) int {
	lower := strings.ToLower(html)
	idx := strings.Index(lower, "<head")
	if idx < 0 {
		return -1
	}
	end := strings.IndexByte(lower[idx:], '>')
	if end < 0 {
		return -1
	}
	return idx + end + 1
}

// validOrigins keeps only the declared entries that are unambiguously origins.
// A dropped entry is silent by design: the view simply does not get that host,
// which is the same outcome as the server never declaring it, and the cockpit
// already reports what was declared so a human can see the difference.
func validOrigins(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		if trimmed := strings.TrimSpace(domain); viewOrigin.MatchString(trimmed) {
			out = append(out, trimmed)
		}
	}
	return out
}

func join(base string, extra []string) string {
	if len(extra) == 0 {
		return base
	}
	return base + " " + strings.Join(extra, " ")
}

func orNone(sources []string) string {
	if len(sources) == 0 {
		return "'none'"
	}
	return strings.Join(sources, " ")
}
