package docfields

import (
	"cmp"
	"context"
	"slices"
)

// MaxListedNames caps each of the library's lists, so a library with many labels still costs a
// search result a bounded number of names.
const MaxListedNames = 30

// ListedName is one label of the library's lists with how many documents hold it.
type ListedName struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
}

// LibraryLists are the types and tags the library's documents hold, most held first: what the
// agent filters by when the operator's words do not spell a label (spec §5, amended 2026-10-23).
type LibraryLists struct {
	Types        []ListedName `json:"types"`
	Tags         []ListedName `json:"tags"`
	OmittedTypes int          `json:"omitted_types,omitempty"`
	OmittedTags  int          `json:"omitted_tags,omitempty"`
}

// LibraryLists counts the whole library, every file type, suggestions included: the counts the
// cockpit's lists page shows, and the values a search filter admits by default.
func (s *Store) LibraryLists(ctx context.Context, identityID string) (LibraryLists, error) {
	library, err := s.List(ctx, identityID, ListQuery{FileType: "all", Limit: 1})
	if err != nil {
		return LibraryLists{}, err
	}
	types, err := s.Labels(ctx, identityID, KindType)
	if err != nil {
		return LibraryLists{}, err
	}
	tags, err := s.Labels(ctx, identityID, KindTag)
	if err != nil {
		return LibraryLists{}, err
	}
	var out LibraryLists
	out.Types, out.OmittedTypes = listed(types, library.Facets["type"])
	out.Tags, out.OmittedTags = listed(tags, library.Facets["tags"])
	return out, nil
}

// listed names the labels some document holds, most documents first and ties by name. A count
// whose label is gone is skipped: it would surface an id the agent cannot filter by.
func listed(labels []Label, counts map[string]int) ([]ListedName, int) {
	names := []ListedName{}
	for _, label := range labels {
		if n := counts[label.ID]; n > 0 {
			names = append(names, ListedName{Name: label.Name, Documents: n})
		}
	}
	slices.SortFunc(names, func(a, b ListedName) int {
		return cmp.Or(cmp.Compare(b.Documents, a.Documents), cmp.Compare(NameKey(a.Name), NameKey(b.Name)))
	})
	if len(names) <= MaxListedNames {
		return names, 0
	}
	return names[:MaxListedNames], len(names) - MaxListedNames
}
