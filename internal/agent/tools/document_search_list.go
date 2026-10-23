package tools

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
)

// documentListProfile is a result that lists rather than ranks: the documents the filters admit,
// most recently added first.
const documentListProfile = "field_list"

// listingPage is where a listing stands: how many documents match in all, and the offset of the
// next page while one remains.
type listingPage struct {
	Matching   int `json:"matching_documents,omitempty"`
	NextOffset int `json:"next_offset,omitempty"`
}

// listDocuments answers a search with no query: the library's own list under the filters and
// document_ids, or all of it with neither, the one the cockpit shows. A document they admit is
// never hidden behind a relevance floor, which is what "show me the documents tagged X" needs.
func (t *DocumentSearch) listDocuments(ctx context.Context, owner string, fields *arcadedb.FieldFilter, args documentSearchArgs) (documents.RetrievalResponse, listingPage, error) {
	if t.Fields == nil {
		return documents.RetrievalResponse{}, listingPage{}, fmt.Errorf("document fields are not configured, so there is no list to read")
	}
	scopes, err := documents.NormalizeSourceScopes(documents.SourceScopesFromContext(ctx))
	if err != nil {
		return documents.RetrievalResponse{}, listingPage{}, err
	}
	keys, prefixes := documents.ArcadeSourceFilters(scopes)
	q := docfields.ListQuery{
		FileType: "all", SourceKeys: keys, SourcePrefixes: prefixes, DocumentIDs: args.DocumentIDs,
		Sort: "-indexed_at", Offset: args.Offset, Limit: effectiveDocumentLimit(args.Limit),
	}
	if fields != nil {
		q.Fields = *fields
	}
	listed, err := t.Fields.List(ctx, owner, q)
	if err != nil {
		return documents.RetrievalResponse{}, listingPage{}, err
	}
	response := documents.RetrievalResponse{
		Profile: documentListProfile, Status: documents.RetrievalComplete,
		Documents: make([]documents.RetrievalDocument, 0, len(listed.Rows)),
	}
	for _, row := range listed.Rows {
		response.Documents = append(response.Documents, listedDocument(row))
	}
	// A page past the end is empty while the filters still admit documents: only a total of zero
	// is the filters admitting nothing, and only when something narrowed the list. Unnarrowed, an
	// empty list is an empty library.
	narrowed := fields != nil || len(args.DocumentIDs) > 0 || len(scopes) > 0
	if listed.Total == 0 && narrowed {
		response.Abstained, response.AbstentionReason = true, documents.AbstainedFilterAdmitsNothing
	}
	page := listingPage{Matching: listed.Total}
	if next := args.Offset + len(listed.Rows); len(listed.Rows) > 0 && next < listed.Total {
		page.NextOffset = next
	}
	return response, page, nil
}

// listedDocument is a library row as a search document: no passage, so it is opened to be read.
func listedDocument(row docfields.DocumentRow) documents.RetrievalDocument {
	size := row.SizeBytes
	doc := documents.RetrievalDocument{
		DocumentID: row.ID, Title: row.FileName, SourceKey: row.SourceKey, OriginalSHA256: row.RawSHA256,
		SizeBytes: &size, RequiresOpen: true,
		Evidence: []documents.RetrievalEvidence{}, Passages: []documents.RetrievalPassage{},
	}
	if at, err := arcadedb.ParseDateTime(row.IndexedAt); err == nil && !at.IsZero() {
		doc.IndexedAt = &at
	}
	return doc
}
