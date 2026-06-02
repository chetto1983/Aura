// Package web is the Phase 7 shared engine behind the web_search and web_fetch
// tools. It owns the full request path so the two thin tool adapters in
// internal/agent/tools stay free of cross-cutting concerns:
//
//   - searxng client  — query build, JSON parse, domain post-filter (D-07..D-14)
//   - ssrf classifier — net/netip blocklist, fail-closed public-web-only (D-24..D-28)
//   - dns pin cache   — per-conversation TTL pin, anti-rebinding (D-25)
//   - pinned transport — dial-only-the-pinned-IP + manual redirect revalidate (D-29)
//   - html extraction — readability → markdown + link dedup (D-17..D-22)
//   - cache           — in-memory TTL, disk opt-in (D-31..D-33)
//   - errors          — the D-38 stable enum + sanitized, non-leaky shapes
//
// Security posture is fail-closed: local/internal targets stay blocked, no
// allowlist escape hatch lands in Phase 7 (D-30), and model-visible errors carry
// no IP / host / header / body / redirect-chain detail (D-27).
//
// The two extraction dependencies (D-20 / roadmap Amendment #3) are anchored
// here so they survive `go mod tidy` ahead of the html.go implementation in a
// later wave: codeberg.org/readeck/go-readability/v2 (FromReader → Article) feeds
// its node tree straight into github.com/JohannesKaufmann/html-to-markdown/v2
// (ConvertNode), with golang.org/x/net/html as the shared *html.Node bridge type.
package web

import (
	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

// extractionDeps is a compile-time anchor that keeps the D-20 extraction modules
// in go.mod before html.go consumes them. It exercises the exact entry points the
// real extractor will use — readability.FromReader, html.Node, and
// htmltomarkdown.ConvertNode — so the dependency graph the later wave needs is
// already pinned and buildable. Remove once html.go imports these directly.
var (
	_ = readability.FromReader
	_ = htmltomarkdown.ConvertNode
	_ html.Node
)
