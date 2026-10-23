package tools

import (
	"context"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
)

// documentListProfile is a result that lists rather than ranks: the documents the filters admit,
// most recently added first.
const documentListProfile = "field_list"

// listDocuments answers a search with no query: the library's own list under the filters, the
// one the cockpit shows. A document the filters admit is never hidden behind a relevance floor,
// which is what "show me the documents tagged X" needs. It returns the listing and how many
// documents match in all.
func (t *DocumentSearch) listDocuments(ctx context.Context, owner string, fields *arcadedb.FieldFilter, limit int) (documents.RetrievalResponse, int, error) {
	scopes, err := documents.NormalizeSourceScopes(documents.SourceScopesFromContext(ctx))
	if err != nil {
		return documents.RetrievalResponse{}, 0, err
	}
	keys, prefixes := documents.ArcadeSourceFilters(scopes)
	listed, err := t.Fields.List(ctx, owner, docfields.ListQuery{
		Fields: *fields, FileType: "all", SourceKeys: keys, SourcePrefixes: prefixes, Sort: "-indexed_at", Limit: limit,
	})
	if err != nil {
		return documents.RetrievalResponse{}, 0, err
	}
	response := documents.RetrievalResponse{
		Profile: documentListProfile, Status: documents.RetrievalComplete,
		Documents: make([]documents.RetrievalDocument, 0, len(listed.Rows)),
	}
	for _, row := range listed.Rows {
		response.Documents = append(response.Documents, listedDocument(row))
	}
	if len(response.Documents) == 0 {
		response.Abstained, response.AbstentionReason = true, documents.AbstainedFilterAdmitsNothing
	}
	return response, listed.Total, nil
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
