package wiki

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// DefaultTOCMaxBytes is the default byte budget for the inline wiki TOC
// injected into the system prompt every turn.
const DefaultTOCMaxBytes = 16 * 1024 // 16 KB

// tocCache is the in-memory cache for the generated wiki TOC string.
// Populated at NewStore time and refreshed on every WritePage/DeletePage.
type tocCache struct {
	mu  sync.RWMutex
	val string
}

func (c *tocCache) get() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.val
}

func (c *tocCache) set(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.val = v
}

// GetCachedTOC returns the most recently generated wiki TOC string.
// Returns "" when the wiki is empty or the store has not yet run RebuildTOC.
func (s *Store) GetCachedTOC() string { return s.toc.get() }

// RebuildTOC regenerates the in-memory TOC cache from the current on-disk
// pages. Safe to call concurrently. Called at boot time (NewStore) and after
// every WritePage / DeletePage.
func (s *Store) RebuildTOC() {
	toc, err := GenerateTOC(s, DefaultTOCMaxBytes)
	if err != nil {
		s.logger.Warn("wiki: TOC rebuild failed", "error", err)
		return
	}
	s.toc.set(toc)
}

// GenerateTOC builds a compact wiki TOC string from store pages.
//
// Output format:
//
//	## Wiki TOC
//	[[slug]] — Title — first-paragraph-summary
//	[[slug]] — Title
//	...
//
// Pages are ranked: Tags containing "EXTRACTED" (case-insensitive) first,
// then by UpdatedAt recency. When total exceeds maxBytes, lower-ranked
// entries are dropped and an overflow marker appended. Pass maxBytes=0
// to disable the budget cap.
func GenerateTOC(store *Store, maxBytes int) (string, error) {
	slugs, err := store.ListPages()
	if err != nil {
		return "", fmt.Errorf("toc: list pages: %w", err)
	}
	entries := buildTOCEntries(store, slugs)

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].extracted != entries[j].extracted {
			return entries[i].extracted
		}
		return entries[i].updatedAt.After(entries[j].updatedAt)
	})

	return renderTOC(entries, maxBytes), nil
}

// tocEntry is the per-page record used while building the TOC.
type tocEntry struct {
	slug      string
	title     string
	summary   string
	updatedAt time.Time
	extracted bool // true when Tags contains "EXTRACTED" (case-insensitive)
}

func buildTOCEntries(store *Store, slugs []string) []tocEntry {
	entries := make([]tocEntry, 0, len(slugs))
	for _, slug := range slugs {
		page, err := store.ReadPage(slug)
		if err != nil {
			continue
		}
		e := tocEntry{
			slug:    slug,
			title:   page.Title,
			summary: firstBodyParagraph(page.Body, 120),
		}
		if t, err := time.Parse(time.RFC3339, page.UpdatedAt); err == nil {
			e.updatedAt = t
		}
		for _, tag := range page.Tags {
			if strings.EqualFold(tag, "EXTRACTED") {
				e.extracted = true
				break
			}
		}
		entries = append(entries, e)
	}
	return entries
}

func renderTOC(entries []tocEntry, maxBytes int) string {
	var sb strings.Builder
	sb.WriteString("## Wiki TOC\n")
	overflow := 0
	for _, e := range entries {
		line := "[[" + e.slug + "]] — " + e.title
		if e.summary != "" {
			line += " — " + e.summary
		}
		line += "\n"
		if maxBytes > 0 && sb.Len()+len(line) > maxBytes {
			overflow++
			continue
		}
		sb.WriteString(line)
	}
	if overflow > 0 {
		fmt.Fprintf(&sb, "... [%d pages not in TOC — use search action=search to find them]\n", overflow)
	}
	return sb.String()
}

// sortedCategoryKeys returns map keys in sorted order.
func sortedCategoryKeys(m map[string][]indexEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// firstBodyParagraph extracts the first non-heading, non-empty content line
// from body and truncates it at maxChars runes. Returns "" when none found.
func firstBodyParagraph(body string, maxChars int) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "---") || strings.HasPrefix(line, "|") {
			continue
		}
		if utf8.RuneCountInString(line) > maxChars {
			runes := []rune(line)
			line = string(runes[:maxChars]) + "…"
		}
		return line
	}
	return ""
}
