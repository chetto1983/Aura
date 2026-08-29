package agui

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/chetto1983/aura/internal/mcp"
)

// artifact_render_html.go prepares an agent-delivered HTML artifact for rendering in
// the cockpit: it strips the tags that phone home WITHOUT a script, then arms the
// document with the sealed-view Content-Security-Policy and the shims a document
// needs to survive an opaque origin.
//
// Why this exists at all: the artifacts panel used to hand the raw bytes to an
// <iframe srcdoc>. A srcdoc document INHERITS the embedder's CSP and has no base URL
// of its own, so the artifact could never carry a policy and the cockpit could never
// tighten its own without blanking every artifact. Serving the bytes from a real URL
// (assets_render_api.go) and framing it with src= inverts that: the response carries
// its own policy, and the embedder's is irrelevant to it.
//
// The sandbox attribute on the frame is still the primary wall — it withholds the
// same-origin token, so scripts run in an opaque origin with no access to the
// operator's cookies or the cockpit DOM. What the sandbox does NOT stop is a document
// with no scripts at all reaching the network: <meta http-equiv="refresh">, <base>,
// and the <link rel=preload> family all fetch before a single line of JS runs, and an
// artifact's markup is downstream of whatever the agent read while producing it. Those
// four are removed here, and connect-src 'none' in the policy closes the scripted half.

// preloadRel is the <link rel> family that fetches on parse. Each one is a request the
// document makes before any script exists, so each one is an exfiltration channel in a
// no-script document; none of them has a legitimate use in a self-contained artifact.
var preloadRel = map[string]struct{}{
	"preload":       {},
	"prefetch":      {},
	"dns-prefetch":  {},
	"preconnect":    {},
	"modulepreload": {},
}

// artifactRuntimeShim restores the two APIs an opaque origin takes away.
//
// A sandboxed frame without allow-same-origin has no storage partition, so
// window.localStorage THROWS SecurityError on access in Chromium rather than
// returning null — and a generated dashboard that reads a saved tab or theme on its
// first line dies there, before it mounts, showing a blank page for a reason that
// looks nothing like storage. The shim swaps in an in-memory store when the real one
// is unreachable. navigator.clipboard is likewise denied, so a copy button silently
// does nothing; execCommand still works inside the user's own click.
//
// Both are installed defensively (every access wrapped) because a browser that DOES
// grant storage must keep the real one.
const artifactRuntimeShim = `
(function () {
  var memory = function () {
    var m = new Map();
    return {
      get length() { return m.size; },
      clear: function () { m.clear(); },
      getItem: function (k) { k = String(k); return m.has(k) ? m.get(k) : null; },
      key: function (i) { return Array.from(m.keys())[Number(i)] ?? null; },
      removeItem: function (k) { m.delete(String(k)); },
      setItem: function (k, v) { m.set(String(k), String(v)); }
    };
  };
  ['localStorage', 'sessionStorage'].forEach(function (name) {
    try {
      var probe = '__aura_artifact_probe__';
      window[name].setItem(probe, probe);
      window[name].removeItem(probe);
    } catch (e) {
      try {
        var store = memory();
        Object.defineProperty(window, name, { configurable: true, get: function () { return store; } });
      } catch (e2) { /* a browser that forbids the redefinition keeps the throwing one */ }
    }
  });
  var copy = function (text) {
    return new Promise(function (resolve, reject) {
      var root = document.body || document.documentElement;
      if (!root || typeof document.execCommand !== 'function') { reject(new Error('clipboard unavailable')); return; }
      var ta = document.createElement('textarea');
      ta.value = String(text == null ? '' : text);
      ta.setAttribute('readonly', '');
      ta.style.cssText = 'position:fixed;top:0;left:-9999px;opacity:0';
      root.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (e) { ta.remove(); reject(e); return; }
      ta.remove();
      ok ? resolve() : reject(new Error('clipboard denied'));
    });
  };
  try {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      get: function () { return { writeText: copy }; }
    });
  } catch (e) {
    try { navigator.clipboard.writeText = copy; } catch (e2) { /* nothing else to try */ }
  }
})();
`

// artifactRenderCSP is the policy an artifact document is served under.
//
// It is the MCP sealed-view floor, not a second policy invented here: an agent-written
// HTML artifact and an MCP view are the same object under the same threat — untrusted
// markup that must render as a document and reach nothing. mcp.ViewPolicy with no
// declared domains yields exactly that floor (default-src 'none', inline script and
// style only, data:/blob: for media, connect-src 'none', no base-uri, no form-action).
// Deriving it keeps ONE definition of "sealed" in the tree; a change to the floor
// reaches artifacts too, which is the intended coupling.
//
// frame-ancestors is appended because default-src does not cover it and a meta tag
// cannot carry it: it only has meaning as a response header, which is precisely what
// this document now has and a srcdoc never did.
func artifactRenderCSP() string {
	return mcp.ViewPolicy{}.ContentSecurityPolicy() + "; frame-ancestors 'self'"
}

// prepareArtifactHTML scrubs the no-script fetch vectors out of an artifact, retargets
// its external links, and returns the document armed with the sealed-view CSP meta and
// the opaque-origin shims.
//
// Parsing is done with a real HTML parser rather than a regex for the reason every
// markup filter learns eventually: `<meta http-equiv = "refresh">`, mixed case, and
// missing quotes are all valid HTML and all defeat a pattern. The parser sees the same
// tree the browser will.
func prepareArtifactHTML(raw string) (string, error) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", err
	}
	scrubArtifactNode(doc)
	// The shim is inserted as the head's FIRST child rather than concatenated onto the
	// rendered string, for an ordering reason the tests caught: ArmedHTML injects the policy
	// at the head boundary, so anything prepended to the whole document ends up OUTSIDE the
	// policy it is supposed to run under. In the tree the shim is unambiguously the first
	// thing in <head>, which puts the meta immediately before it and every artifact script
	// after it.
	if head := findHead(doc); head != nil {
		head.InsertBefore(newInlineScript(artifactRuntimeShim), head.FirstChild)
	}

	var out strings.Builder
	if err := html.Render(&out, doc); err != nil {
		return "", err
	}
	return mcp.ArmedHTML(out.String(), artifactRenderCSP()), nil
}

// findHead returns the document's <head>, which html.Parse synthesises even for a fragment.
// It is nil only for input the parser could not shape into a document at all, in which case
// the caller still gets a policy — ArmedHTML prepends the meta when there is no head.
func findHead(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Head {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findHead(c); found != nil {
			return found
		}
	}
	return nil
}

// newInlineScript builds a <script> whose body html.Render emits raw, exactly as authored —
// script content is raw text in the HTML syntax, so the JS is not entity-escaped on the way
// out.
func newInlineScript(js string) *html.Node {
	script := &html.Node{Type: html.ElementNode, DataAtom: atom.Script, Data: "script"}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: js})
	return script
}

// scrubArtifactNode walks the parsed tree, dropping the fetch-on-parse tags and making
// every off-document anchor open in a new tab without leaking the opener.
func scrubArtifactNode(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		if c.Type == html.ElementNode {
			if dropArtifactElement(c) {
				n.RemoveChild(c)
				continue
			}
			if c.DataAtom == atom.A {
				retargetArtifactAnchor(c)
			}
		}
		scrubArtifactNode(c)
	}
}

// dropArtifactElement reports whether an element fetches on parse and must not survive.
func dropArtifactElement(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Base:
		// A <base> rewrites every relative URL in the document, including back at the
		// cockpit's own origin, and it also defeats the "self-contained" assumption the
		// policy rests on.
		return true
	case atom.Meta:
		return strings.EqualFold(strings.TrimSpace(attrValue(n, "http-equiv")), "refresh")
	case atom.Link:
		for rel := range strings.FieldsSeq(strings.ToLower(attrValue(n, "rel"))) {
			if _, found := preloadRel[rel]; found {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// retargetArtifactAnchor sends an off-document link to a new tab with the opener
// severed. Without target=_blank the link would navigate the FRAME, replacing the
// artifact with a remote page still wearing the artifact's sandbox; without noopener
// that page would hold a handle on the frame it came from.
func retargetArtifactAnchor(n *html.Node) {
	href := strings.TrimSpace(attrValue(n, "href"))
	if href == "" || !isExternalHref(href) {
		return
	}
	setAttr(n, "target", "_blank")
	rel := strings.Fields(strings.ToLower(attrValue(n, "rel")))
	for _, token := range []string{"noopener", "noreferrer"} {
		if !slices.Contains(rel, token) {
			rel = append(rel, token)
		}
	}
	setAttr(n, "rel", strings.Join(rel, " "))
}

// isExternalHref reports whether an href leaves the document — the schemes a browser
// navigates away on, plus the protocol-relative form.
func isExternalHref(href string) bool {
	if strings.HasPrefix(href, "//") {
		return true
	}
	lower := strings.ToLower(href)
	for _, scheme := range []string{"http://", "https://", "mailto:", "tel:"} {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

func attrValue(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if strings.EqualFold(n.Attr[i].Key, key) {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}
