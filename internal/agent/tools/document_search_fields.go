package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
)

// DocumentFieldFilters is what document_search needs of the document fields: the label ids behind
// the names it is given, how many documents each filter admits, and the types and tags the library
// holds. *docfields.Store in production.
type DocumentFieldFilters interface {
	ResolveNames(ctx context.Context, identityID string, kind docfields.Kind, names []string) ([]string, map[string][]string, error)
	Admissions(ctx context.Context, identityID string, f arcadedb.FieldFilter) ([]arcadedb.FieldAdmission, error)
	LibraryLists(ctx context.Context, identityID string) (docfields.LibraryLists, error)
}

// documentFieldArgs are the filters by the fields the operator keeps on documents (spec decision
// 5 of docs/superpowers/specs/2026-10-18-document-fields-design.md). Names, not ids: the agent
// reads them from the operator, and the tool resolves them against the operator's lists.
type documentFieldArgs struct {
	Type         []string `json:"type"`
	Counterparty []string `json:"counterparty"`
	Tags         []string `json:"tags"`
	TagsMode     string   `json:"tags_mode"`
	DateFrom     string   `json:"date_from"`
	DateTo       string   `json:"date_to"`
	// IncludeUnreviewed defaults to true, so a library nobody reviewed still answers.
	IncludeUnreviewed *bool `json:"include_unreviewed"`
}

func (a documentFieldArgs) empty() bool {
	return len(a.Type)+len(a.Counterparty)+len(a.Tags) == 0 && a.DateFrom == "" && a.DateTo == ""
}

// documentFilterReport is what one filter did: the agent reads from it whether an empty answer
// is the filter's or the library's, and whether a match rests on suggestions.
type documentFilterReport struct {
	Name               string              `json:"name"`
	Values             []string            `json:"values"`
	AdmittedDocuments  int                 `json:"admitted_documents"`
	RestsOnSuggestions bool                `json:"rests_on_suggestions"`
	UnknownNames       map[string][]string `json:"unknown_names,omitempty"`
}

// fieldFilter resolves the named filters to label ids and reports, in the order a search applies
// them, what each admits on its own. A named filter none of whose names is on a list admits
// nothing, and is reported so rather than dropped: dropping it would widen the search.
func (t *DocumentSearch) fieldFilter(ctx context.Context, identityID string, args documentFieldArgs) (*arcadedb.FieldFilter, []documentFilterReport, error) {
	if args.empty() {
		return nil, nil, nil
	}
	if t.Fields == nil {
		return nil, nil, fmt.Errorf("document fields are not configured, so type, counterparty, tags and dates cannot filter")
	}
	filter := arcadedb.FieldFilter{DateFrom: args.DateFrom, DateTo: args.DateTo, IncludeUnreviewed: true}
	if args.IncludeUnreviewed != nil {
		filter.IncludeUnreviewed = *args.IncludeUnreviewed
	}
	switch args.TagsMode {
	case "", "any":
	case "all":
		filter.TagsAll = true
	default:
		return nil, nil, fmt.Errorf("tags_mode is any or all, not %q", args.TagsMode)
	}
	reports := map[arcadedb.FieldDimension]*documentFilterReport{}
	for _, named := range []struct {
		dimension arcadedb.FieldDimension
		kind      docfields.Kind
		names     []string
		ids       *[]string
	}{
		{arcadedb.FieldType, docfields.KindType, args.Type, &filter.Types},
		{arcadedb.FieldCounterparty, docfields.KindCounterparty, args.Counterparty, &filter.Counterparties},
		{arcadedb.FieldTags, docfields.KindTag, args.Tags, &filter.Tags},
	} {
		if len(named.names) == 0 {
			continue
		}
		ids, unknown, err := t.Fields.ResolveNames(ctx, identityID, named.kind, named.names)
		if err != nil {
			return nil, nil, err
		}
		*named.ids = ids
		report := &documentFilterReport{Name: string(named.dimension), Values: trimmed(named.names)}
		if len(unknown) > 0 {
			report.UnknownNames = unknown
		}
		reports[named.dimension] = report
	}
	if args.DateFrom != "" || args.DateTo != "" {
		reports[arcadedb.FieldDate] = &documentFilterReport{Name: string(arcadedb.FieldDate), Values: dateBounds(args.DateFrom, args.DateTo)}
	}
	admissions, err := t.Fields.Admissions(ctx, identityID, filter)
	if err != nil {
		return nil, nil, err
	}
	for _, admission := range admissions {
		report, ok := reports[admission.Dimension]
		if !ok {
			continue
		}
		report.AdmittedDocuments = admission.Admitted
		report.RestsOnSuggestions = admission.Admitted > admission.Reviewed
	}
	ordered := make([]documentFilterReport, 0, len(reports))
	for _, dimension := range []arcadedb.FieldDimension{arcadedb.FieldType, arcadedb.FieldCounterparty, arcadedb.FieldTags, arcadedb.FieldDate} {
		if report, ok := reports[dimension]; ok {
			ordered = append(ordered, *report)
		}
	}
	return &filter, ordered, nil
}

// libraryLists names the library's types and tags for the result. A list that cannot be read is
// left out rather than failing a search that can still answer.
func (t *DocumentSearch) libraryLists(ctx context.Context, identityID string) *docfields.LibraryLists {
	if t.Fields == nil {
		return nil
	}
	lists, err := t.Fields.LibraryLists(ctx, identityID)
	if err != nil {
		slog.Warn("document_search: library lists unavailable", "err", err)
		return nil
	}
	return &lists
}

// admitsNothing reports a filter no document passes: searching under it can only come back empty.
func admitsNothing(reports []documentFilterReport) bool {
	for _, report := range reports {
		if report.AdmittedDocuments == 0 {
			return true
		}
	}
	return false
}

// filterAbstention is the answer to a search one of whose filters admits nothing.
func filterAbstention(query string) documents.RetrievalResponse {
	return documents.RetrievalResponse{
		Query: query, Status: documents.RetrievalComplete, Abstained: true,
		AbstentionReason: documents.AbstainedFilterAdmitsNothing, Documents: []documents.RetrievalDocument{},
	}
}

func trimmed(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

func dateBounds(from, to string) []string {
	var out []string
	if from != "" {
		out = append(out, ">= "+from)
	}
	if to != "" {
		out = append(out, "<= "+to)
	}
	return out
}
