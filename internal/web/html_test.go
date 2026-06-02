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
