package docfields

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/agnivade/levenshtein"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// maxClosestNames bounds what an unknown name is answered with: enough to correct a typo, not the
// whole list.
const maxClosestNames = 3

// ResolveNames turns the names a caller gives into the ids of one kind's labels. A name matches
// trimmed and case-folded, and exactly (NameKey). A name that matches nothing comes back with up to
// three of the names on the list that resemble it, closest first, so the caller can correct itself
// or drop the filter without a second query. None resembling it is an empty list.
func (s *Store) ResolveNames(ctx context.Context, identityID string, kind Kind, names []string) ([]string, map[string][]string, error) {
	labels, err := s.Labels(ctx, identityID, kind)
	if err != nil {
		return nil, nil, err
	}
	ids, unknown := resolve(labels, names)
	return ids, unknown, nil
}

// Admissions counts, for each filter f applies, the indexed documents it admits on its own.
func (s *Store) Admissions(ctx context.Context, identityID string, f arcadedb.FieldFilter) ([]arcadedb.FieldAdmission, error) {
	c, err := s.client(ctx, identityID)
	if err != nil {
		return nil, err
	}
	return c.FieldAdmissions(ctx, f)
}

func resolve(labels []Label, names []string) ([]string, map[string][]string) {
	byKey := make(map[string]string, len(labels))
	for _, label := range labels {
		byKey[NameKey(label.Name)] = label.ID
	}
	var ids []string
	unknown := map[string][]string{}
	for _, name := range names {
		if id, ok := byKey[NameKey(name)]; ok {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
			continue
		}
		unknown[strings.TrimSpace(name)] = closest(NameKey(name), labels)
	}
	return ids, unknown
}

func closest(key string, labels []Label) []string {
	var ranked []Label
	for _, label := range labels {
		if similar(key, NameKey(label.Name)) {
			ranked = append(ranked, label)
		}
	}
	slices.SortStableFunc(ranked, func(a, b Label) int {
		if d := levenshtein.ComputeDistance(key, NameKey(a.Name)) - levenshtein.ComputeDistance(key, NameKey(b.Name)); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	out := make([]string, 0, maxClosestNames)
	for _, label := range ranked[:min(maxClosestNames, len(ranked))] {
		out = append(out, label.Name)
	}
	return out
}

// minContainedRunes keeps containment from matching everything: a one-letter name is in most names.
const minContainedRunes = 3

// similar is whether two name keys are near enough for one to be a correction of the other: an edit
// distance of at most half the longer, or one containing the other. Measured on the lab VM, a name
// with nothing like it on the list was still answered with the list's only tag (spec §5).
func similar(a, b string) bool {
	if levenshtein.ComputeDistance(a, b)*2 <= max(utf8.RuneCountInString(a), utf8.RuneCountInString(b)) {
		return true
	}
	if utf8.RuneCountInString(a) < minContainedRunes || utf8.RuneCountInString(b) < minContainedRunes {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}
