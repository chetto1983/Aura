package display

import (
	"net/url"
	"strings"

	"github.com/chetto1983/aura/internal/web"
)

// normalizeWebSearch maps a web_search result ([]web.Result, searxng.go:30-45) to
// a web_result Payload (DISP-01). One WebItem per hit, Domain derived from the URL
// host, the metadata tier (Score/PublishedAt/Thumbnail) lifted from ResultMetadata
// when present, and a stable URL-keyed RefID assigned via the per-turn Registry so
// each hit is a consulted source (D-05). An empty result set still yields a
// recognized (true) web_result payload — "no hits" is a valid typed display.
func normalizeWebSearch(toolCallID string, results []web.Result, reg *Registry) (Payload, bool) {
	items := make([]WebItem, 0, len(results))
	for i := range results {
		r := &results[i]
		item := WebItem{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Snippet,
			Domain:  hostOf(r.URL),
		}
		if r.Metadata != nil {
			item.Score = r.Metadata.Score
			item.PublishedAt = r.Metadata.PublishedAt
			item.Thumbnail = r.Metadata.Thumbnail
		}
		item.RefID = reg.Add(KindWebResult, r.Title, r.URL, r.Snippet, false)
		items = append(items, item)
	}
	return Payload{
		Type:       KindWebResult,
		ToolCallID: toolCallID,
		WebResults: items,
		Sources:    reg.Sources(),
	}, true
}

// normalizeWebFetch maps a web_fetch result (web.Page, fetcher.go:22-28) to a
// document Payload (DISP-01): the fetched markdown rendered through the sanitized
// MarkdownText host. The fetched page is registered as a consulted source so it
// shows in the Source Explorer.
func normalizeWebFetch(toolCallID string, page web.Page, reg *Registry) (Payload, bool) {
	reg.Add(KindDocument, page.Title, page.URL, "", false)
	return Payload{
		Type:       KindDocument,
		ToolCallID: toolCallID,
		Title:      page.Title,
		Document: &Document{
			Title:     page.Title,
			URL:       page.URL,
			ContentMD: page.ContentMD,
		},
		Sources: reg.Sources(),
	}, true
}

// hostOf returns the lower-cased host of rawURL, or "" when it cannot be parsed
// (the Domain field is a display hint, never a security decision).
func hostOf(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
