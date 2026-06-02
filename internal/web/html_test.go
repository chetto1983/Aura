//go:build !web_integration

package web

import (
	"net/url"
	"strings"
	"testing"
)

const richHTML = `<!DOCTYPE html><html><head><title>The Real Title</title></head>
<body>
<nav>NAVLINK_FIXTURE_MARKER home about</nav>
<article>
<h1>The Real Title</h1>
<p>This is the genuine readable article body. It contains several sentences of real
prose so the readability heuristic keeps it as the main content rather than discarding
it as boilerplate. The quick brown fox jumps over the lazy dog repeatedly to pad length.</p>
<p>A second paragraph with a <a href="/relative/page">relative link</a> and an
<a href="https://other.example/abs">absolute link</a> and a duplicate
<a href="/relative/page">relative link again</a>.</p>
</article>
<footer>FOOTER_FIXTURE_MARKER copyright 2026</footer>
</body></html>`

func TestExtractMarkdown_BodyAndLinks(t *testing.T) {
	pageURL, _ := url.Parse("https://src.example/article")
	title, md, links, warning, err := ExtractMarkdown([]byte(richHTML), pageURL)
	if err != nil {
		t.Fatalf("ExtractMarkdown: %v", err)
	}
	if warning != "" {
		t.Errorf("rich article should have no warning, got %q", warning)
	}
	if title != "The Real Title" {
		t.Errorf("title = %q, want article title only", title)
	}
	if !strings.Contains(md, "genuine readable article body") {
		t.Errorf("markdown missing article body: %q", md)
	}
	if strings.Contains(md, "FOOTER_FIXTURE_MARKER") || strings.Contains(md, "NAVLINK_FIXTURE_MARKER") {
		t.Errorf("markdown leaked nav/footer boilerplate: %q", md)
	}
	// Links must be deduped, absolute, resolved against pageURL.
	if !containsStr(links, "https://src.example/relative/page") {
		t.Errorf("relative link not resolved to absolute: %v", links)
	}
	if !containsStr(links, "https://other.example/abs") {
		t.Errorf("absolute link missing: %v", links)
	}
	if countStr(links, "https://src.example/relative/page") != 1 {
		t.Errorf("duplicate link not deduped: %v", links)
	}
}

// citeHTML mimics a Wikipedia-style article: a "From Wikipedia" banner, body prose
// with inline <sup> citation anchors (which convert to [\[n\]](#cite_note-n)), a
// fragment-only intra-page anchor, and a References <ol> tail — the exact noise the
// Gate-3 cleanup strips.
const citeHTML = `<!DOCTYPE html><html><head><title>Knowledge Graph</title></head>
<body>
<article>
<p>From Wikipedia, the free encyclopedia</p>
<h1>Knowledge Graph</h1>
<p>A knowledge graph is a structured representation of facts about real-world
entities, relationships, and semantic descriptions used widely in search and AI.
This paragraph is intentionally long enough that the readability heuristic keeps it
as the main article content rather than discarding it as boilerplate text.<sup><a href="#cite_note-1">[1]</a></sup>
It links to an <a href="https://other.example/abs">external source</a> and to a
<a href="#section-history">jump anchor</a> within the page.<sup><a href="#cite_note-2">[2]</a></sup></p>
<h2>References</h2>
<ol>
<li id="cite_note-1">REFERENCE_LIST_MARKER first citation, journal of graphs.</li>
<li id="cite_note-2">REFERENCE_LIST_MARKER second citation, proceedings of nowhere.</li>
</ol>
</article>
</body></html>`

func TestExtractMarkdown_StripsCitationNoise(t *testing.T) {
	pageURL, _ := url.Parse("https://en.wikipedia.org/wiki/Knowledge_graph")
	_, md, links, _, err := ExtractMarkdown([]byte(citeHTML), pageURL)
	if err != nil {
		t.Fatalf("ExtractMarkdown: %v", err)
	}
	if !strings.Contains(md, "structured representation of facts") {
		t.Errorf("article prose missing from cleaned markdown: %q", md)
	}
	if strings.Contains(md, "#cite_note") || strings.Contains(md, "#cite_ref") {
		t.Errorf("citation anchors not stripped: %q", md)
	}
	if strings.Contains(md, "REFERENCE_LIST_MARKER") {
		t.Errorf("references tail not truncated: %q", md)
	}
	if strings.Contains(md, "From Wikipedia") {
		t.Errorf("Wikipedia boilerplate line not stripped: %q", md)
	}
	for _, l := range links {
		if strings.Contains(l, "#") {
			t.Errorf("fragment-only link not filtered: %q (all: %v)", l, links)
		}
	}
	if !containsStr(links, "https://other.example/abs") {
		t.Errorf("absolute link missing after cleanup: %v", links)
	}
}

func TestExtractMarkdown_LowContent(t *testing.T) {
	pageURL, _ := url.Parse("https://src.example/thin")
	_, md, _, warning, err := ExtractMarkdown([]byte(`<html><body><p>tiny</p></body></html>`), pageURL)
	if err != nil {
		t.Fatalf("low content must not be an error: %v", err)
	}
	if warning != "low_content" {
		t.Errorf("warning = %q, want low_content", warning)
	}
	_ = md // markdown may be short/empty but the call succeeds (D-22)
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func countStr(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}
