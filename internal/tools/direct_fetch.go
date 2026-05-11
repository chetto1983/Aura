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

	"golang.org/x/net/html"
)

const maxDirectFetchResponseBytes = 2 << 20
const maxDirectFetchTextChars = 12000
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
		return webFetchResponse{}, fmt.Errorf("parse URL: %w", err)
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
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" || strings.Contains(strings.ToLower(string(body[:min(len(body), 512)])), "<html") {
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

func parseHTMLFetchResult(body []byte, base *url.URL) webFetchResponse {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return webFetchResponse{Title: base.String(), Content: normalizeWhitespace(string(body))}
	}
	var title string
	var text strings.Builder
	links := make([]string, 0)

	var walk func(*html.Node, bool)
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "style", "noscript", "svg":
				skip = true
			case "title":
				title = normalizeWhitespace(nodeText(n))
				skip = true
			case "a":
				if href := attrValue(n, "href"); href != "" && len(links) < maxDirectFetchLinks {
					if resolved := resolveLink(base, href); resolved != "" {
						links = append(links, resolved)
					}
				}
			case "p", "div", "section", "article", "main", "li", "br", "h1", "h2", "h3":
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
	}
	walk(root, false)

	return webFetchResponse{
		Title:   title,
		Content: normalizeWhitespace(text.String()),
		Links:   uniqueStrings(links),
	}
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
