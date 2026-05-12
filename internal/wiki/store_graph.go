package wiki

import "fmt"

// loadGraphIndex reads every wiki page from disk and rebuilds the
// in-memory adjacency index (Wave 2 — GRAPH-02) in one shot. Called
// once during NewStore so retrieval / write hooks have a warm index
// from the first query. Cheap at ~30 pages today; would only need
// batching above ~10K pages, where streaming + chunked rebuild would
// matter.
//
// Per-page parse failures are logged and skipped — a bad page on disk
// should not block the rest of the index from warming. The materialized
// graph layer (BuildGraph) will surface the same pages as broken-ref
// emitters at the next maintenance pass.
func (s *Store) loadGraphIndex() error {
	slugs, err := s.ListPages()
	if err != nil {
		return fmt.Errorf("list pages for graph index: %w", err)
	}
	pages := make(map[string]*Page, len(slugs))
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			s.logger.Warn("graph index skip page", "slug", slug, "error", err)
			continue
		}
		pages[slug] = page
	}
	s.graphIndex.LoadFromPages(pages)
	s.logger.Info("graph index warmed", "nodes", s.graphIndex.NodeCount())
	return nil
}

// GraphIndex exposes the in-memory adjacency index for use by the
// retrieval layer (search_memory) and any future graph-aware tools.
// Returns nil only if the store was never initialized — production
// callers will always get a real index from NewStore.
func (s *Store) GraphIndex() *GraphIndex { return s.graphIndex }
