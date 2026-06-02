package web

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

// lowContentRunes is the PRD threshold below which readable text is flagged
// low_content (D-22) — a warning, NOT an error.
const lowContentRunes = 250

// citationAnchorRe matches a STRUCTURAL markdown link whose target is an intra-page
// citation fragment (#cite_note-* / #cite_ref-*), e.g. `[\[1\]](#cite_note-1)`. These
// are wiki citation superscripts that convert to noise; the link form is structural
// (not natural-language prose), so a regex over it is safe.
var citationAnchorRe = regexp.MustCompile(`\[(?:[^\[\]\\]|\\.)*\]\(#cite_(?:note|ref)[^)]*\)`)

// referencesHeadingRe matches the first markdown heading (any level) whose text is
// exactly one of the reference-tail section words. Everything from that heading to
// EOF is the citations bulk and is dropped (the user's "references tail" intent).
var referencesHeadingRe = regexp.MustCompile(`(?im)^#{1,6}\s+(?:References|Notes|Citations|Bibliography)\s*$`)

// wikiBoilerplateRe matches the leading "From Wikipedia, the free encyclopedia"
// banner line readability leaves at the top of a wiki article.
var wikiBoilerplateRe = regexp.MustCompile(`(?im)^\s*From Wikipedia, the free encyclopedia\s*$`)

// ExtractMarkdown turns already-fetched, size-gated HTML bytes into clean markdown
// plus deduped absolute links. It NEVER fetches — readability.FromReader runs on
// the bytes the hardened transport already pulled, so the SSRF gate is never
// bypassed (the self-fetching readability entry point is forbidden, T-07-22).
// pageURL anchors relative link resolution (D-19). title is art.Title() only (no
// byline/excerpt/site/time, D-18). Readable text shorter than lowContentRunes
// returns markdown with warning="low_content" and a nil error (D-22).
func ExtractMarkdown(body []byte, pageURL *url.URL) (title, markdown string, links []string, warning string, err error) {
	art, perr := readability.FromReader(bytes.NewReader(body), pageURL)
	if perr != nil {
		return "", "", nil, "", &WebError{Code: CodeExtractionFailed, Message: "could not extract readable content"}
	}
	title = art.Title()

	if art.Node == nil {
		return title, "", nil, "low_content", nil
	}

	md, cErr := convertNode(art.Node)
	if cErr != nil {
		return "", "", nil, "", &WebError{Code: CodeExtractionFailed, Message: "could not render readable content"}
	}
	md = cleanMarkdown(md)
	links = extractLinks(art.Node, pageURL)

	// Re-evaluate low_content AFTER cleanup so a page that becomes thin once the
	// citation/references noise is stripped is correctly flagged (D-22).
	if utf8.RuneCountInString(strings.TrimSpace(md)) < lowContentRunes {
		warning = "low_content"
	}
	return title, md, links, warning, nil
}

// cleanMarkdown post-processes the converted markdown to match a cleaner web_fetch
// payload (Gate-3 2026-06-02): it strips inline #cite_note/#cite_ref citation anchors,
// the leading "From Wikipedia" boilerplate, and truncates the references/notes tail.
// All patterns are STRUCTURAL markdown forms, never natural-language prose.
func cleanMarkdown(md string) string {
	md = wikiBoilerplateRe.ReplaceAllString(md, "")
	md = citationAnchorRe.ReplaceAllString(md, "")
	if loc := referencesHeadingRe.FindStringIndex(md); loc != nil {
		md = md[:loc[0]]
	}
	return strings.TrimSpace(md)
}

// convertNode renders the readability node tree to markdown. ConvertNode consumes
// the *html.Node directly; on the rare chance it chokes, fall back to a RenderHTML
// round-trip through ConvertString (A2 fallback).
func convertNode(node *html.Node) (string, error) {
	if b, err := htmltomarkdown.ConvertNode(node); err == nil {
		return string(b), nil
	}
	var buf bytes.Buffer
	if rErr := html.Render(&buf, node); rErr != nil {
		return "", rErr
	}
	return htmltomarkdown.ConvertString(buf.String())
}

// extractLinks walks the READABLE node tree (not the raw page) for <a href>,
// resolves each against pageURL, and dedups into normalized absolute URL strings
// (D-19 — strings, not {text,url} objects). A malformed href is skipped.
func extractLinks(root *html.Node, pageURL *url.URL) []string {
	seen := make(map[string]struct{})
	var links []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key != "href" {
					continue
				}
				raw := strings.TrimSpace(a.Val)
				ref, perr := url.Parse(raw)
				if perr != nil {
					continue
				}
				// Skip fragment-only / intra-page anchors (e.g. #cite_note-*,
				// #cite_ref-*, table-of-contents jumps): no scheme, no host, no
				// path — just a fragment. They are not navigable targets (D-19).
				if ref.Scheme == "" && ref.Host == "" && ref.Path == "" && ref.Fragment != "" {
					continue
				}
				abs := pageURL.ResolveReference(ref).String()
				if _, ok := seen[abs]; !ok {
					seen[abs] = struct{}{}
					links = append(links, abs)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return links
}
