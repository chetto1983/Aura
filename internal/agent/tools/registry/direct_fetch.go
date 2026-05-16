package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	readability "github.com/go-shiori/go-readability"
	"golang.org/x/net/html"
)

const maxDirectFetchResponseBytes = 2 << 20

// maxDirectFetchTextChars caps the extracted text from a direct web fetch
// before downstream formatters touch it. Aligned with
// config.DefaultMaxToolResultChars (24000) per Phase-F: cap LATENCY and
// COST, not CAPABILITY.
const maxDirectFetchTextChars = 24000

// maxDirectFetchLinks bounds link extraction per fetched page — anti-DoS on
// pathological HTML, NOT a capability throttle (a typical wiki/article page
// has 10-30 outbound links).
const maxDirectFetchLinks = 50

// DirectWebFetchTool fetches http(s) URLs with strict local bounds while
// preserving the stable LLM-facing web_fetch tool name.
type DirectWebFetchTool struct {
	client *http.Client
}

func NewDirectWebFetchTool() *DirectWebFetchTool {
	transport := &http.Transport{
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &DirectWebFetchTool{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects (>5)")
				}
				if req.URL == nil {
					return errors.New("redirect target missing URL")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
				}
				return nil
			},
		},
	}
}

// safeDialContext is the SSRF gate for web_fetch. It resolves the host, refuses
// to dial loopback / private / link-local / cloud-metadata addresses, and then
// dials the validated IP directly — defeating DNS-rebinding races where the
// resolver returns a public IP at validation time and a private IP at dial time.
//
// Two env knobs override the default-deny:
//   - AURA_WEB_FETCH_ALLOW_LOOPBACK=1 — permit 127.0.0.0/8 / ::1 (tests, dev).
//   - AURA_WEB_FETCH_ALLOW_HOSTS=host1,host2 — exact-hostname bypass (ops).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split host: %w", err)
	}
	if isAllowedFetchHost(host) {
		return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
	}
	resolver := net.DefaultResolver
	ips, err := resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for host")
	}
	allowLoopback := webFetchAllowLoopback()
	for _, ip := range ips {
		if !isPublicFetchIP(ip, allowLoopback) {
			return nil, fmt.Errorf("web_fetch: refusing to dial host (resolved to non-public address)")
		}
	}
	// Dial the first validated IP explicitly so the kernel cannot pick a different
	// (potentially private) record between LookupIP and Dial.
	return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// isPublicFetchIP returns true when ip is safe to dial from web_fetch.
// allowLoopback opens loopback only when the operator opted in.
func isPublicFetchIP(ip net.IP, allowLoopback bool) bool {
	if ip == nil {
		return false
	}
	if allowLoopback && ip.IsLoopback() {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// AWS/GCP/Azure metadata service. Already covered by IsLinkLocalUnicast()
	// for 169.254/16, but explicit equality stays as defense-in-depth.
	if v4 := ip.To4(); v4 != nil && v4.Equal(net.IPv4(169, 254, 169, 254)) {
		return false
	}
	return true
}

func isAllowedFetchHost(host string) bool {
	raw := strings.TrimSpace(os.Getenv("AURA_WEB_FETCH_ALLOW_HOSTS"))
	if raw == "" {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(host))
	for _, entry := range strings.Split(raw, ",") {
		if entry = strings.ToLower(strings.TrimSpace(entry)); entry != "" && entry == target {
			return true
		}
	}
	return false
}

func webFetchAllowLoopback() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AURA_WEB_FETCH_ALLOW_LOOPBACK"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (t *DirectWebFetchTool) Name() string { return "web_fetch" }

func (t *DirectWebFetchTool) Description() string {
	return "Fetch a web page by URL and return its title, main content, and discovered links."
}

func (t *DirectWebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch.",
			},
		},
		"required": []string{"url"},
	}
}

func (t *DirectWebFetchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	targetURL, err := requiredString(args, "url")
	if err != nil {
		return "", err
	}
	result, err := t.fetch(ctx, targetURL)
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	return truncateForToolContext(formatFetchResult(targetURL, result), maxWebToolChars), nil
}

func (t *DirectWebFetchTool) fetch(ctx context.Context, targetURL string) (webFetchResponse, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		// Deliberately drop the wrapped error: url.Parse's Error wraps the
		// raw input string, and the input may contain a credentialed query
		// (?token=...). The error is destined for the conversation archive,
		// which is the durable log channel per CLAUDE.md.
		return webFetchResponse{}, fmt.Errorf("URL parse failed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return webFetchResponse{}, fmt.Errorf("only http and https URLs are allowed")
	}
	if parsed.Host == "" {
		return webFetchResponse{}, fmt.Errorf("URL host is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return webFetchResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.1")
	req.Header.Set("User-Agent", "AuraBot/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return webFetchResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return webFetchResponse{}, fmt.Errorf("HTTP %d from %s", resp.StatusCode, parsed.Host)
	}

	body, truncated, err := readBounded(resp.Body, maxDirectFetchResponseBytes)
	if err != nil {
		return webFetchResponse{}, fmt.Errorf("read response: %w", err)
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var result webFetchResponse
	// Prefer http.DetectContentType — it handles BOMs, leading whitespace,
	// and DOCTYPE prefixes correctly. The previous substring sniff missed
	// pages with a long comment header before <html>.
	sniffLen := min(len(body), 512)
	detected := http.DetectContentType(body[:sniffLen])
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" || strings.HasPrefix(detected, "text/html") {
		result = parseHTMLFetchResult(body, resp.Request.URL)
	} else {
		result = webFetchResponse{Title: resp.Request.URL.String(), Content: normalizeWhitespace(string(body))}
	}
	if truncated {
		if result.Content != "" {
			result.Content += "\n\n"
		}
		result.Content += "[truncated: response body exceeded fetch limit]"
	}
	if len(result.Content) > maxDirectFetchTextChars {
		result.Content = strings.TrimSpace(result.Content[:maxDirectFetchTextChars]) + "\n\n[truncated: extracted text exceeded fetch limit]"
	}
	return result, nil
}

func readBounded(r io.Reader, maxBytes int) ([]byte, bool, error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, int64(maxBytes)+1))
	if err != nil {
		return nil, false, err
	}
	b := buf.Bytes()
	if n > int64(maxBytes) {
		return b[:maxBytes], true, nil
	}
	return b, false, nil
}

// readabilityMinLength sets the floor for Readability's "this is an
// article" verdict. Below this, we treat the page as not-an-article and
// fall back to the basic DOM walker so short pages (login walls, error
// pages, redirect stubs, single-line API JSON) still surface something
// useful.
const readabilityMinLength = 250

// parseHTMLFetchResult is the article-extractor + Markdown-converter
// pipeline. Per Aura research 2026-05-12: status-quo DOM walker is
// production-last for LLM consumption; the Readability+Turndown stack
// is what Claude Code, Firecrawl, and most 2026 production agents use.
//
//	1. go-readability isolates the article subtree (drops nav, footer,
//	   cookie banners, share buttons via Mozilla Readability scoring).
//	2. html-to-markdown converts that subtree to Markdown preserving
//	   heading hierarchy, code blocks, lists, and emphasis.
//	3. Title is taken from Article.Title (often better than <title>).
//	4. Links are extracted from the cleaned subtree only.
//
// Fallback: if Readability rejects the page (Length below threshold or
// error), drop to the basic DOM walker so we still return something
// for non-article pages (API JSON, error pages, redirect stubs).
func parseHTMLFetchResult(body []byte, base *url.URL) webFetchResponse {
	article, err := readability.FromReader(bytes.NewReader(body), base)
	if err == nil && article.Length >= readabilityMinLength && strings.TrimSpace(article.Content) != "" {
		md, mdErr := htmltomarkdown.ConvertString(article.Content)
		if mdErr == nil && strings.TrimSpace(md) != "" {
			title := article.Title
			if title == "" {
				title = base.String()
			}
			return webFetchResponse{
				Title:   collapseInline(title),
				Content: structuredNormalize(md),
				Links:   extractArticleLinks(article.Content, base),
			}
		}
	}
	return basicHTMLFetchResult(body, base)
}

// extractArticleLinks pulls <a href> from the readability-cleaned HTML
// subtree only. This is the single largest noise reduction vs. the old
// "first 50 anchors in the document" rule — nav/footer/share links
// never enter the result.
func extractArticleLinks(htmlContent string, base *url.URL) []string {
	root, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}
	links := make([]string, 0)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(links) >= maxDirectFetchLinks {
			return
		}
		if n.Type == html.ElementNode && strings.ToLower(n.Data) == "a" {
			if href := attrValue(n, "href"); href != "" {
				if resolved := resolveLink(base, href); resolved != "" {
					links = append(links, resolved)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return uniqueStrings(links)
}

// basicHTMLFetchResult is the DOM walker kept as a fallback for
// pages where Readability rejects the body (too short, not an article,
// or parse error). Preserves the existing behaviour for non-article
// pages so we never return a worse result than before the refactor.
func basicHTMLFetchResult(body []byte, base *url.URL) webFetchResponse {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return webFetchResponse{Title: base.String(), Content: collapseInline(string(body))}
	}
	var title string
	var text strings.Builder
	links := make([]string, 0)

	// textWalkCap bounds the in-memory text buffer during the HTML walk. The
	// post-walk truncation cap (maxDirectFetchTextChars) is what reaches the
	// LLM; the 4x overshoot keeps that path simple while still cutting peak
	// RAM on payloads dominated by text nodes.
	const textWalkCap = maxDirectFetchTextChars * 4
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, skip bool) {
		if text.Len() > textWalkCap {
			return
		}
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			// Drop entirely: scripts, styles, embedded media metadata.
			case "script", "style", "noscript", "svg":
				skip = true
			// Drop entirely: HTML5 landmarks that are NEVER article body.
			// Skipping these at parse time cuts cookie banners, top-nav,
			// site-wide footers, and sidebars — the biggest source of
			// noise an LLM has to ignore when summarizing a fetched page.
			case "nav", "header", "footer", "aside", "form":
				skip = true
			case "title":
				title = collapseInline(nodeText(n))
				skip = true
			case "a":
				if href := attrValue(n, "href"); href != "" && len(links) < maxDirectFetchLinks {
					if resolved := resolveLink(base, href); resolved != "" {
						links = append(links, resolved)
					}
				}
			// Headings get markdown prefixes so the LLM sees the article
			// outline. Each heading also gets its own line via the
			// surrounding newlines below.
			case "h1":
				text.WriteString("\n\n# ")
			case "h2":
				text.WriteString("\n\n## ")
			case "h3":
				text.WriteString("\n\n### ")
			case "h4":
				text.WriteString("\n\n#### ")
			case "h5":
				text.WriteString("\n\n##### ")
			case "h6":
				text.WriteString("\n\n###### ")
			case "li":
				text.WriteString("\n- ")
			case "p", "div", "section", "article", "main", "br":
				text.WriteString("\n")
			}
		}
		if !skip && n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteString(" ")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}
		// Close the heading line so the next text doesn't run into it.
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				text.WriteString("\n")
			}
		}
	}
	walk(root, false)

	return webFetchResponse{
		Title:   title,
		Content: structuredNormalize(text.String()),
		Links:   uniqueStrings(links),
	}
}

// collapseInline flattens all whitespace to single spaces. Use for spans
// that must live on a single line (page title, inline attribute values).
func collapseInline(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// structuredNormalize keeps the line structure produced by parseHTMLFetchResult
// while collapsing intra-line whitespace runs. Limits consecutive blank lines
// to two so a page littered with empty <div>s doesn't produce a 100-line wall
// of nothing.
func structuredNormalize(s string) string {
	var out strings.Builder
	blanks := 0
	for _, line := range strings.Split(s, "\n") {
		trimmed := collapseInline(line)
		if trimmed == "" {
			blanks++
			if blanks <= 1 {
				out.WriteString("\n")
			}
			continue
		}
		blanks = 0
		out.WriteString(trimmed)
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			sb.WriteString(cur.Data)
			sb.WriteString(" ")
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return sb.String()
}

func attrValue(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func resolveLink(base *url.URL, href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.Scheme == "javascript" || u.Scheme == "mailto" {
		return ""
	}
	return base.ResolveReference(u).String()
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
