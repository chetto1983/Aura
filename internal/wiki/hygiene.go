package wiki

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MemoryHygieneOptions controls the automated memory graph cleanup pass.
// Dry-run is the default; Apply=true writes repairs and generated hub pages.
type MemoryHygieneOptions struct {
	Apply bool
}

// MemoryHygieneReport summarizes what the pass found and, when Apply=true,
// what it changed.
type MemoryHygieneReport struct {
	Pages         int          `json:"pages"`
	BrokenLinks   []BrokenLink `json:"broken_links,omitempty"`
	Orphans       []string     `json:"orphans,omitempty"`
	PlannedHubs   []string     `json:"planned_hubs,omitempty"`
	CreatedHubs   []string     `json:"created_hubs,omitempty"`
	RepairedLinks []LinkRepair `json:"repaired_links,omitempty"`
	RenamedPages  []PageRename `json:"renamed_pages,omitempty"`
	TouchedPages  []string     `json:"touched_pages,omitempty"`
}

// BrokenLink is a missing [[slug]] or related frontmatter reference.
type BrokenLink struct {
	From     string `json:"from"`
	Target   string `json:"target"`
	Location string `json:"location"`
}

// LinkRepair records a broken reference rewritten to an existing canonical page.
type LinkRepair struct {
	From        string `json:"from"`
	Target      string `json:"target"`
	Replacement string `json:"replacement"`
	Location    string `json:"location"`
}

// PageRename records a safe semantic rename for an opaque memory page.
type PageRename struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type memoryPage struct {
	slug string
	page *Page
}

// CleanMemory audits and optionally repairs wiki graph hygiene. It keeps
// operational files out of memory, repairs obvious alias links, creates shared
// category hub pages for repeated missing concepts, and connects isolated pages
// to those hubs.
func (s *Store) CleanMemory(ctx context.Context, opts MemoryHygieneOptions) (*MemoryHygieneReport, error) {
	pages, err := s.memoryPages()
	if err != nil {
		return nil, err
	}

	report := &MemoryHygieneReport{Pages: len(pages)}
	known, byCategory := organizeMemoryPages(pages)
	plannedRenames := s.planOpaqueSourceRenames(pages, known)
	report.RenamedPages = plannedRenames
	if opts.Apply && len(plannedRenames) > 0 {
		if err := s.applyPageRenames(ctx, plannedRenames); err != nil {
			return report, err
		}
		pages, err = s.memoryPages()
		if err != nil {
			return report, err
		}
		report.Pages = len(pages)
		known, byCategory = organizeMemoryPages(pages)
	}

	report.Pages = len(pages)
	if !opts.Apply {
		report.RenamedPages = s.planOpaqueSourceRenames(pages, known)
	}

	incoming := make(map[string]int)
	outgoing := make(map[string]int)
	missingByTarget := make(map[string][]BrokenLink)
	missingByTargetCategory := make(map[string]map[string]int)

	for _, item := range pages {
		targets := pageGraphRefs(item.page)
		for _, ref := range targets {
			if _, ok := known[ref.target]; ok {
				incoming[ref.target]++
				outgoing[item.slug]++
				continue
			}
			broken := BrokenLink{From: item.slug, Target: ref.target, Location: ref.location}
			report.BrokenLinks = append(report.BrokenLinks, broken)
			missingByTarget[ref.target] = append(missingByTarget[ref.target], broken)
			category := cleanCategory(item.page.Category)
			if missingByTargetCategory[ref.target] == nil {
				missingByTargetCategory[ref.target] = make(map[string]int)
			}
			missingByTargetCategory[ref.target][category]++
		}
	}

	for _, item := range pages {
		if incoming[item.slug] == 0 && outgoing[item.slug] == 0 {
			report.Orphans = append(report.Orphans, item.slug)
		}
	}
	sort.Strings(report.Orphans)

	hubCategory := make(map[string]string)
	for target, categoryCounts := range missingByTargetCategory {
		category, count := dominantCategory(categoryCounts)
		if count >= 2 || len(missingByTarget[target]) >= 2 {
			hubCategory[target] = category
		}
	}
	for category, slugs := range byCategory {
		if len(slugs) < 2 {
			continue
		}
		if existingCategoryHub(category, slugs, known) != "" {
			continue
		}
		needsHub := false
		for _, slug := range slugs {
			if incoming[slug] == 0 && outgoing[slug] == 0 {
				needsHub = true
				break
			}
		}
		if needsHub {
			hubCategory[categoryHubSlug(category)] = category
		}
	}

	replacements := make(map[string]string)
	for target := range missingByTarget {
		if _, createsHub := hubCategory[target]; createsHub {
			continue
		}
		if replacement := bestAliasMatch(target, known); replacement != "" {
			replacements[target] = replacement
		}
	}

	report.PlannedHubs = sortedHubSlugs(hubCategory, known)
	if !opts.Apply {
		report.RepairedLinks = plannedRepairs(missingByTarget, replacements)
		return report, nil
	}

	for _, hubSlug := range report.PlannedHubs {
		category := hubCategory[hubSlug]
		if err := s.writeMemoryHub(ctx, hubSlug, category, byCategory[category], known); err != nil {
			return report, err
		}
		report.CreatedHubs = append(report.CreatedHubs, hubSlug)
	}

	touched := make(map[string]bool)
	for _, item := range pages {
		changed := false
		if stripAutoSourcePreview(item.page) {
			changed = true
		}
		for broken, fixed := range replacements {
			old := "[[" + broken + "]]"
			next := "[[" + fixed + "]]"
			if strings.Contains(item.page.Body, old) {
				item.page.Body = strings.ReplaceAll(item.page.Body, old, next)
				report.RepairedLinks = append(report.RepairedLinks, LinkRepair{From: item.slug, Target: broken, Replacement: fixed, Location: "body"})
				changed = true
			}
			if replaceRelated(item.page, broken, fixed) {
				report.RepairedLinks = append(report.RepairedLinks, LinkRepair{From: item.slug, Target: broken, Replacement: fixed, Location: "related"})
				changed = true
			}
		}

		category := cleanCategory(item.page.Category)
		for _, hubSlug := range report.CreatedHubs {
			if hubCategory[hubSlug] != category || hubSlug == item.slug {
				continue
			}
			if !pageReferencesSlug(item.page, hubSlug) {
				item.page.Related = RelatedAppendSlug(item.page.Related, hubSlug)
				changed = true
			}
		}

		if !changed {
			continue
		}
		item.page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.WritePage(ctx, item.page); err != nil {
			return report, fmt.Errorf("clean memory write %s: %w", item.slug, err)
		}
		touched[item.slug] = true
	}

	report.TouchedPages = sortedStringSet(touched)
	if memoryHygieneChanged(report) {
		s.RebuildIndex(ctx)
		s.AppendLog(ctx, "memory-hygiene", "")
	}
	return report, nil
}

func memoryHygieneChanged(report *MemoryHygieneReport) bool {
	return len(report.RenamedPages) > 0 ||
		len(report.CreatedHubs) > 0 ||
		len(report.RepairedLinks) > 0 ||
		len(report.TouchedPages) > 0
}

func (s *Store) memoryPages() ([]memoryPage, error) {
	slugs, err := s.ListPages()
	if err != nil {
		return nil, fmt.Errorf("clean memory list pages: %w", err)
	}
	sort.Strings(slugs)
	pages := make([]memoryPage, 0, len(slugs))
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			return nil, fmt.Errorf("clean memory read %s: %w", slug, err)
		}
		pages = append(pages, memoryPage{slug: slug, page: page})
	}
	return pages, nil
}
