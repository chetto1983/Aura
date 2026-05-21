package wiki

import "strings"

// SubgraphSnippet returns the raw byte content of the heading section
// identified by subnodeID (format: "<page-slug>#<anchor>"). The snippet
// spans from the first byte of the heading line through the start of the
// next same-or-lower-level heading (or end of body).
//
// Returns nil when subnodeID is not a subnode ID, the page cannot be read,
// or the subnode anchor is not found in the graph index.
func (s *Store) SubgraphSnippet(subnodeID string) []byte {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	if !isSubnodeID(subnodeID) {
		return nil
	}

	pageSlug, anchor, _ := strings.Cut(subnodeID, "#")

	// Retrieve the page meta to find the subnode byte range.
	pageMeta, ok := s.graphIndex.Meta(pageSlug)
	if !ok {
		return nil
	}
	var sub *SubnodeMeta
	for i := range pageMeta.Subnodes {
		if pageMeta.Subnodes[i].AnchorSlug == anchor {
			sub = &pageMeta.Subnodes[i]
			break
		}
	}
	if sub == nil {
		return nil
	}

	// Read the page body from disk to obtain the current byte content.
	page, err := s.ReadPage(pageSlug)
	if err != nil {
		return nil
	}

	body := []byte(page.Body)
	start := sub.ByteStart
	end := sub.ByteEnd
	if start >= len(body) {
		return nil
	}
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}
