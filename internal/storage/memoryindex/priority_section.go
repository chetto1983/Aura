package memoryindex

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	PrioritySectionMaxRules     = 30
	PrioritySectionRuleChars    = 240
	PrioritySectionMaxBytes     = 6000
	PrioritySectionRefreshTurns = 5
)

const prioritySectionHeader = "## Pinned Operational Lessons\n\nApply these Critical/High lessons before choosing or calling tools:\n\n"

type PriorityOperationalStore interface {
	FetchPinnedOperational(ctx context.Context, limit int) ([]Document, error)
}

type PriorityOperationalRecallMarker interface {
	MarkOperationalRecalled(ctx context.Context, ids []string, recalledAt time.Time) error
}

// RenderPrioritySection renders the Critical/High operational rules pinned into
// the system prompt. Empty stores and store errors both omit the section.
func RenderPrioritySection(ctx context.Context, store PriorityOperationalStore) string {
	if store == nil {
		return ""
	}
	docs, err := store.FetchPinnedOperational(ctx, PrioritySectionMaxRules*2)
	if err != nil || len(docs) == 0 {
		return ""
	}
	content, ids := renderPrioritySectionDocs(docs)
	if content != "" {
		if marker, ok := store.(PriorityOperationalRecallMarker); ok {
			_ = marker.MarkOperationalRecalled(ctx, ids, time.Now().UTC())
		}
	}
	return content
}

// PrioritySectionCache batches DB-driven prompt refreshes so ordinary memory
// writes do not invalidate the model's prompt cache every turn.
type PrioritySectionCache struct {
	mu                 sync.Mutex
	content            string
	lastRefreshTurnIdx int
	hasContent         bool
	invalidated        bool
}

func (c *PrioritySectionCache) Render(ctx context.Context, store PriorityOperationalStore, currentTurnIdx int) string {
	if c == nil {
		return RenderPrioritySection(ctx, store)
	}
	c.mu.Lock()
	if c.hasContent && !c.invalidated && currentTurnIdx >= c.lastRefreshTurnIdx && currentTurnIdx-c.lastRefreshTurnIdx < PrioritySectionRefreshTurns {
		content := c.content
		c.mu.Unlock()
		return content
	}
	c.mu.Unlock()

	content := RenderPrioritySection(ctx, store)
	c.mu.Lock()
	c.content = content
	c.lastRefreshTurnIdx = currentTurnIdx
	c.hasContent = true
	c.invalidated = false
	c.mu.Unlock()
	return content
}

func (c *PrioritySectionCache) InvalidatePinSection() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.invalidated = true
	c.mu.Unlock()
}

// RenderPinnedSectionWithCache renders the priority section for a thread,
// using caches (a *sync.Map of *PrioritySectionCache keyed by thread) to avoid
// recomputing across turns. cacheKey is strings.TrimSpace(threadID); when empty
// defaultKey is used instead. Shared by the Telegram and web invocation builders
// so the cache logic is not duplicated across packages.
func RenderPinnedSectionWithCache(ctx context.Context, store PriorityOperationalStore, caches *sync.Map, threadID, defaultKey string, turnIdx int) string {
	if store == nil {
		return ""
	}
	cacheKey := strings.TrimSpace(threadID)
	if cacheKey == "" {
		cacheKey = defaultKey
	}
	cacheAny, _ := caches.LoadOrStore(cacheKey, &PrioritySectionCache{})
	cache, ok := cacheAny.(*PrioritySectionCache)
	if !ok || cache == nil {
		return RenderPrioritySection(ctx, store)
	}
	return cache.Render(ctx, store, turnIdx)
}

// InvalidatePinnedSectionInCache marks the priority section stale for a thread,
// so the next RenderPinnedSectionWithCache call re-fetches from the store.
func InvalidatePinnedSectionInCache(caches *sync.Map, threadID, defaultKey string) {
	cacheKey := strings.TrimSpace(threadID)
	if cacheKey == "" {
		cacheKey = defaultKey
	}
	if cacheAny, ok := caches.Load(cacheKey); ok {
		if cache, ok := cacheAny.(*PrioritySectionCache); ok {
			cache.InvalidatePinSection()
		}
	}
}

func renderPrioritySectionDocs(docs []Document) (string, []string) {
	docs = append([]Document(nil), docs...)
	sort.SliceStable(docs, func(i, j int) bool {
		leftRank := priorityRank(docs[i].Priority)
		rightRank := priorityRank(docs[j].Priority)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if !docs[i].UpdatedAt.Equal(docs[j].UpdatedAt) {
			return docs[i].UpdatedAt.After(docs[j].UpdatedAt)
		}
		return docs[i].ID < docs[j].ID
	})

	var sb strings.Builder
	sb.WriteString(prioritySectionHeader)
	rules := 0
	ids := make([]string, 0, PrioritySectionMaxRules)
	for _, doc := range docs {
		priority := NormalizePriority(doc.Priority)
		if priority != PriorityCritical && priority != PriorityHigh {
			continue
		}
		title := compactPriorityText(doc.Title)
		body := truncatePriorityRule(compactPriorityText(doc.Body), PrioritySectionRuleChars)
		if title == "" && body == "" {
			continue
		}
		var line string
		switch {
		case title != "" && body != "":
			line = fmt.Sprintf("- [%s] %s: %s\n", priority, title, body)
		case title != "":
			line = fmt.Sprintf("- [%s] %s\n", priority, title)
		default:
			line = fmt.Sprintf("- [%s] %s\n", priority, body)
		}
		if sb.Len()+len(line) > PrioritySectionMaxBytes {
			break
		}
		sb.WriteString(line)
		ids = append(ids, doc.ID)
		rules++
		if rules >= PrioritySectionMaxRules {
			break
		}
	}
	if rules == 0 {
		return "", nil
	}
	return strings.TrimRight(sb.String(), "\n"), ids
}

func priorityRank(priority string) int {
	switch NormalizePriority(priority) {
	case PriorityCritical:
		return 0
	case PriorityHigh:
		return 1
	default:
		return 2
	}
}

func compactPriorityText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncatePriorityRule(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
