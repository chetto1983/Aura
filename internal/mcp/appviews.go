package mcp

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// appviews.go holds the documents themselves: apps.go reads what a server
// DECLARED, this file keeps what it SERVED. One catalog per process, filled once
// per mount, read by whatever surface renders — so the HTTP layer never has to
// hold an MCP session, and a view fetch never races a redial.
//
// Server-agnostic like apps.go: a document is identified by (mounted server name,
// resource URI) and nothing else.

// MaxViewBytes bounds one view document. A `ui://` resource is an HTML page a
// browser will parse, and its size is chosen by the server, not by Aura — an
// unbounded read here is a mounted server deciding how much of the host's memory
// it may occupy. 2 MiB is far above any real view (the two measured on
// 2026-08-17 are ~20 KB) and far below a problem.
const MaxViewBytes = 2 << 20

// ViewDocument is one `ui://` resource as served, with the framing declaration
// that came with it.
type ViewDocument struct {
	Server      string
	ResourceURI string
	HTML        string
	Policy      ViewPolicy
}

type viewKey struct{ server, uri string }

// ViewCatalog stores the view documents read at mount time.
//
// Every method tolerates a nil receiver, and that is load-bearing rather than
// defensive habit: it lets every mount path take a *ViewCatalog unconditionally
// and lets a caller that renders nothing pass nil, instead of each path growing
// an "if views are enabled" branch that could be forgotten in one of them.
type ViewCatalog struct {
	mu   sync.RWMutex
	docs map[viewKey]ViewDocument
}

// NewViewCatalog returns an empty catalog. A nil *ViewCatalog is equally usable
// and renders nothing, so this is only needed where documents will be stored.
func NewViewCatalog() *ViewCatalog {
	return &ViewCatalog{docs: map[viewKey]ViewDocument{}}
}

// Put stores a document, replacing any earlier one for the same key. It rejects a
// document whose URI is not a `ui://` one, whose HTML is empty, or whose HTML
// exceeds MaxViewBytes — the three shapes that must never reach a browser.
func (c *ViewCatalog) Put(doc ViewDocument) error {
	if c == nil {
		return nil
	}
	if !IsAppURI(doc.ResourceURI) {
		return fmt.Errorf("mcp view %q: not a %s resource", doc.ResourceURI, AppURIScheme)
	}
	if strings.TrimSpace(doc.HTML) == "" {
		return fmt.Errorf("mcp view %q: empty document", doc.ResourceURI)
	}
	if len(doc.HTML) > MaxViewBytes {
		return fmt.Errorf("mcp view %q: %d bytes exceeds the %d-byte cap", doc.ResourceURI, len(doc.HTML), MaxViewBytes)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.docs[viewKey{server: doc.Server, uri: doc.ResourceURI}] = doc
	return nil
}

// Get returns the document a server serves at uri.
func (c *ViewCatalog) Get(server, uri string) (ViewDocument, bool) {
	if c == nil {
		return ViewDocument{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	doc, ok := c.docs[viewKey{server: server, uri: uri}]
	return doc, ok
}

// HasServer reports whether a server catalogued any view at mount. It is the
// check a view's callback route makes before letting a browser name a server: a
// server that never served a document has no view to be calling back from.
func (c *ViewCatalog) HasServer(server string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for key := range c.docs {
		if key.server == server {
			return true
		}
	}
	return false
}

// URIs lists the catalogued URIs, sorted, as "<server> <uri>" pairs. It exists so
// a boot log can state what a host will actually render — a view that failed to
// read is otherwise invisible until a human clicks something that is not there.
func (c *ViewCatalog) URIs() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.docs))
	for key := range c.docs {
		out = append(out, key.server+" "+key.uri)
	}
	sort.Strings(out)
	return out
}
