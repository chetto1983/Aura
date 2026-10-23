package docfields

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// The library, as the Documenti view lists it: every indexed document, whether or not anyone has
// labelled it, with its fields. A document is named by its IndexedDocument.search_document_id,
// the id document_search returns too, and its record is created when it is first written.

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// sortColumns are the orders a list may ask for, each by its IndexedDocument property.
var sortColumns = map[string]string{"file_name": "file_name", "indexed_at": "indexed_at", "document_date": "df_date"}

// DocumentRow is one document of the library with its fields.
type DocumentRow struct {
	ID          string     `json:"id"`
	SourceKey   string     `json:"source_key"`
	FileName    string     `json:"file_name"`
	FileType    string     `json:"file_type"`
	SizeBytes   int64      `json:"size_bytes"`
	IndexedAt   string     `json:"indexed_at"`
	RawSHA256   string     `json:"raw_sha256,omitempty"`
	RecordID    string     `json:"record_id,omitempty"`
	Values      []Value    `json:"values"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	NeedsReview bool       `json:"needs_review"`
}

// DocumentDetail is one document with what the operator rejected and its history.
type DocumentDetail struct {
	DocumentRow
	Rejections []ValueRef `json:"rejections"`
	History    []Event    `json:"history"`
}

// ListQuery filters, orders and pages the library. Fields holds the label ids a document must
// hold, Exclude the ones it must not (its types, counterparties and tags), and Unassigned the
// fields that must hold nothing: "type", "counterparty", "tags". Fields.IncludeUnreviewed decides,
// for all three, whether suggestions count. SearchMode selects filename matching or the content
// index. Inbox includes records without reviewed_at, independently of NeedsReview suggestions.
// FileType defaults to documents; all explicitly includes media, code and unknown extensions.
// SourceKeys and SourcePrefixes are a scope: the objects named, or any below a folder.
// Sort is a column of sortColumns, descending with a leading "-".
type ListQuery struct {
	Fields         arcadedb.FieldFilter
	Exclude        arcadedb.FieldFilter
	Unassigned     []string
	Search         string
	SearchMode     string
	FileType       string
	Inbox          bool
	SourceKeys     []string
	SourcePrefixes []string
	Custom         []CustomFilter
	NeedsReview    bool
	Sort           string
	Offset         int
	Limit          int
}

// unassignable maps a field a filter may ask to be empty to its stamped property; tags are empty
// when the list is.
var unassignable = map[string]string{"type": "df_type", "counterparty": "df_counterparty", "tags": "df_tags"}

// Facets counts, per field, how many documents of a result hold each value.
type Facets map[string]map[string]int

// ListResult is one page of the library, the size of the whole result and its facets.
type ListResult struct {
	Rows   []DocumentRow `json:"rows"`
	Total  int           `json:"total"`
	Facets Facets        `json:"facets"`
}

const documentRowColumns = "search_document_id, source_key, file_name, size_bytes, indexed_at, raw_sha256"

// List reads a page of the library. The total and the facets count the whole result, so the
// view can say how many documents a filter holds before the operator pages through them.
func (s *Store) List(ctx context.Context, identityID string, q ListQuery) (ListResult, error) {
	order, err := listOrder(q.Sort)
	if err != nil {
		return ListResult{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	limit = min(limit, maxPageSize)
	c, where, params, err := s.libraryScope(ctx, identityID, q)
	if err != nil {
		return ListResult{}, err
	}
	result, ingested, err := libraryCounts(ctx, c, where, params)
	if err != nil || !ingested {
		return result, err
	}
	page, err := c.Query(ctx, fmt.Sprintf("SELECT %s FROM IndexedDocument WHERE %s ORDER BY %s SKIP %d LIMIT %d",
		documentRowColumns, where, order, max(q.Offset, 0), limit), params)
	if err != nil {
		return ListResult{}, fmt.Errorf("read the library: %w", err)
	}
	result.Rows, err = withFields(ctx, c, decodeRows(page))
	return result, err
}

// libraryCounts is the size and the facets of every document where admits. A library nothing was
// ingested into has no IndexedDocument type yet: it counts nothing, and ingested is false.
func libraryCounts(ctx context.Context, c *arcadedb.Client, where string, params map[string]any) (ListResult, bool, error) {
	result := ListResult{Rows: []DocumentRow{}, Facets: Facets{"type": {}, "counterparty": {}, "tags": {}, "unassigned": {}}}
	all, err := c.Query(ctx, "SELECT df_type, df_counterparty, df_tags FROM IndexedDocument WHERE "+where, params)
	if arcadedb.IsMissingType(err, arcadedb.IndexedDocumentType) {
		return result, false, nil
	}
	if err != nil {
		return ListResult{}, false, fmt.Errorf("count the library: %w", err)
	}
	result.Total = len(all)
	for _, row := range all {
		countFacet(result.Facets["type"], row["df_type"])
		countFacet(result.Facets["counterparty"], row["df_counterparty"])
		tags := listOf(row["df_tags"])
		for _, tag := range tags {
			countFacet(result.Facets["tags"], tag)
		}
		for field, empty := range map[string]bool{"type": str(row["df_type"]) == "", "counterparty": str(row["df_counterparty"]) == "", "tags": len(tags) == 0} {
			if empty {
				result.Facets["unassigned"][field]++
			}
		}
	}
	return result, true, nil
}

// Select is every document a query admits, minus the ones excluded: what "select all in the
// filter" applies to.
func (s *Store) Select(ctx context.Context, identityID string, q ListQuery, exclude []string) ([]string, error) {
	c, where, params, err := s.libraryScope(ctx, identityID, q)
	if err != nil {
		return nil, err
	}
	rows, err := c.Query(ctx, "SELECT search_document_id FROM IndexedDocument WHERE "+where+" ORDER BY search_document_id", params)
	if arcadedb.IsMissingType(err, arcadedb.IndexedDocumentType) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select documents: %w", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := str(row["search_document_id"]); !slices.Contains(exclude, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// Document reads one document, its rejections and its history.
func (s *Store) Document(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	c, err := s.client(ctx, identityID)
	if err != nil {
		return DocumentDetail{}, err
	}
	rows, err := c.Query(ctx, "SELECT "+documentRowColumns+" FROM IndexedDocument WHERE search_document_id = :id",
		map[string]any{"id": documentID})
	if err != nil && !arcadedb.IsMissingType(err, arcadedb.IndexedDocumentType) {
		return DocumentDetail{}, fmt.Errorf("read document: %w", err)
	}
	if len(rows) == 0 {
		return DocumentDetail{}, fmt.Errorf("%w: document %s", ErrNotFound, documentID)
	}
	found, err := withFields(ctx, c, decodeRows(rows))
	if err != nil {
		return DocumentDetail{}, err
	}
	detail := DocumentDetail{DocumentRow: found[0], Rejections: []ValueRef{}, History: []Event{}}
	if detail.RecordID == "" {
		return detail, nil
	}
	f, _, err := byKey(ctx, c, detail.SourceKey)
	if err != nil {
		return DocumentDetail{}, err
	}
	detail.Rejections = append(detail.Rejections, f.Rejections...)
	history, err := s.History(ctx, identityID, detail.RecordID)
	detail.History = append(detail.History, history...)
	return detail, err
}

// ApplyToDocuments changes the named documents by d, as the operator, creating the records of
// documents nobody labelled yet.
func (s *Store) ApplyToDocuments(ctx context.Context, identityID string, documentIDs []string, d Diff) error {
	if err := validateDiff(d); err != nil {
		return err
	}
	d, err := s.adoptInDiff(ctx, identityID, d)
	if err != nil {
		return err
	}
	recordIDs, err := s.documentRecords(ctx, identityID, documentIDs)
	if err != nil {
		return err
	}
	return s.ApplyDiff(ctx, identityID, recordIDs, d, SetByOperator)
}

// ReviewDocuments marks the named documents reviewed.
func (s *Store) ReviewDocuments(ctx context.Context, identityID string, documentIDs []string) error {
	recordIDs, err := s.documentRecords(ctx, identityID, documentIDs)
	if err != nil {
		return err
	}
	return s.MarkReviewed(ctx, identityID, recordIDs)
}

// libraryScope resolves the identity's client and the query's predicate.
func (s *Store) libraryScope(ctx context.Context, identityID string, q ListQuery) (*arcadedb.Client, string, map[string]any, error) {
	params := map[string]any{}
	where, err := arcadedb.FieldPredicate(q.Fields, params)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	extra, err := libraryPredicates(q, params)
	if err != nil {
		return nil, "", nil, err
	}
	c, err := s.client(ctx, identityID)
	if err == nil && q.SearchMode == "content" && strings.TrimSpace(q.Search) != "" {
		var rows []map[string]any
		rows, err = c.Query(ctx, "SELECT name FROM schema:types WHERE name = :name", map[string]any{"name": arcadedb.IndexedDocumentType})
		if err == nil && len(rows) == 0 {
			return c, "1 = 0", params, nil
		}
	}
	return c, strings.Join(append([]string{where}, extra...), " AND "), params, err
}

// libraryPredicates are the list's own filters beside the search's: exclusions, empty fields, the
// name search and the review state. A document holding no value passes an exclusion.
func libraryPredicates(q ListQuery, params map[string]any) ([]string, error) {
	property := func(name string) string {
		if q.Fields.IncludeUnreviewed {
			return name
		}
		return name + "_ok"
	}
	var out []string
	for _, excluded := range []struct {
		property, param string
		ids             []string
	}{{"df_type", "not_types", q.Exclude.Types}, {"df_counterparty", "not_counterparties", q.Exclude.Counterparties}} {
		if len(excluded.ids) > 0 {
			params[excluded.param] = excluded.ids
			out = append(out, fmt.Sprintf("(%[1]s IS NULL OR %[1]s NOT IN :%[2]s)", property(excluded.property), excluded.param))
		}
	}
	if len(q.Exclude.Tags) > 0 {
		params["not_tags"] = q.Exclude.Tags
		out = append(out, fmt.Sprintf("(%[1]s IS NULL OR NOT (%[1]s CONTAINSANY :not_tags))", property("df_tags")))
	}
	for _, field := range q.Unassigned {
		name, ok := unassignable[field]
		if !ok {
			return nil, invalid("unknown field %q to find unassigned", field)
		}
		if field == "tags" {
			out = append(out, fmt.Sprintf("(%[1]s IS NULL OR %[1]s.size() = 0)", property(name)))
		} else {
			out = append(out, property(name)+" IS NULL")
		}
	}
	if err := validateWorkspaceQuery(q); err != nil {
		return nil, err
	}
	if search := strings.TrimSpace(q.Search); search != "" && q.SearchMode != "content" {
		params["search"] = "%" + likeLiteral.Replace(strings.ToLower(search)) + "%"
		out = append(out, "file_name.toLowerCase() LIKE :search")
	}
	if q.NeedsReview {
		out = append(out, "df_needs_review = true")
	}
	out = append(out, workspacePredicates(q, params)...)
	return out, nil
}

// likeLiteral keeps a search's own characters literal: ArcadeDB's LIKE wildcards are % and ?.
var likeLiteral = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `?`, `\?`)

// documentRecords is the record id of each named document, in order, created for those that have
// none: one script for every missing record. A name that is no document of this identity's is
// ErrNotFound, which is what another identity's document is too.
func (s *Store) documentRecords(ctx context.Context, identityID string, documentIDs []string) ([]string, error) {
	ids := slices.Compact(slices.Sorted(slices.Values(documentIDs)))
	if len(ids) == 0 {
		return nil, invalid("no document named")
	}
	c, err := s.client(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := c.Query(ctx, "SELECT source_key FROM IndexedDocument WHERE search_document_id IN :ids", map[string]any{"ids": ids})
	if err != nil && !arcadedb.IsMissingType(err, arcadedb.IndexedDocumentType) {
		return nil, fmt.Errorf("read documents: %w", err)
	}
	if len(rows) != len(ids) {
		return nil, fmt.Errorf("%w: %d of %d documents", ErrNotFound, len(ids)-len(rows), len(ids))
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, str(row["source_key"]))
	}
	return s.recordsFor(ctx, c, identityID, keys)
}

// recordsFor returns the record id of each key, inserting the missing ones in one script. A
// concurrent first use may insert one first; the UNIQUE index refuses the script, and the next
// round reads what it wrote.
func (s *Store) recordsFor(ctx context.Context, c *arcadedb.Client, identityID string, keys []string) ([]string, error) {
	for range 3 {
		rows, err := c.Query(ctx, "SELECT source_key, record_id FROM DocumentFields WHERE source_key IN :keys", map[string]any{"keys": keys})
		if err != nil {
			return nil, fmt.Errorf("read document fields: %w", err)
		}
		held := make(map[string]string, len(rows))
		for _, row := range rows {
			held[str(row["source_key"])] = str(row["record_id"])
		}
		sc := newScript()
		now := s.now().Format(timeLayout)
		for _, key := range keys {
			if held[key] == "" {
				held[key] = uuid.NewString()
				sc.add("INSERT INTO DocumentFields SET source_key = " + sc.bind(key) + ", record_id = " + sc.bind(held[key]) +
					", values = [], rejections = [], tags = [], tags_ok = [], created_at = " + sc.bind(now) + ", updated_at = " + sc.bind(now))
			}
		}
		if len(sc.statements) > 0 {
			if _, err := c.Script(ctx, sc.text(), sc.params); arcadedb.IsDuplicateKey(err) {
				continue
			} else if err != nil {
				return nil, fmt.Errorf("create document fields for %s: %w", identityID, err)
			}
		}
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			out = append(out, held[key])
		}
		return out, nil
	}
	return nil, fmt.Errorf("create document fields for %s: the records kept changing underneath", identityID)
}

// withFields attaches to each row its record's values, when it has one, in one request.
func withFields(ctx context.Context, c *arcadedb.Client, rows []DocumentRow) ([]DocumentRow, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.SourceKey)
	}
	records, err := c.Query(ctx, "SELECT FROM DocumentFields WHERE source_key IN :keys", map[string]any{"keys": keys})
	if err != nil {
		return nil, fmt.Errorf("read document fields: %w", err)
	}
	byKey := make(map[string]Fields, len(records))
	for _, record := range records {
		f := decodeFields(record)
		byKey[f.SourceKey] = f
	}
	for i := range rows {
		f, ok := byKey[rows[i].SourceKey]
		if !ok {
			continue
		}
		rows[i].RecordID, rows[i].ReviewedAt = f.RecordID, f.ReviewedAt
		rows[i].Values = append(rows[i].Values, f.Values...)
		rows[i].NeedsReview = f.projection().NeedsReview
	}
	return rows, nil
}

func decodeRows(rows []map[string]any) []DocumentRow {
	out := make([]DocumentRow, 0, len(rows))
	for _, row := range rows {
		size, _ := row["size_bytes"].(float64)
		out = append(out, DocumentRow{ID: str(row["search_document_id"]), SourceKey: str(row["source_key"]),
			FileName: str(row["file_name"]), FileType: fileTypeFromName(str(row["file_name"])), SizeBytes: int64(size), IndexedAt: str(row["indexed_at"]), RawSHA256: str(row["raw_sha256"]), Values: []Value{}})
	}
	return out
}

func listOrder(sort string) (string, error) {
	if sort == "" {
		return "indexed_at DESC, source_key", nil
	}
	column, ok := sortColumns[strings.TrimPrefix(sort, "-")]
	if !ok {
		return "", invalid("unknown sort %q", sort)
	}
	if strings.HasPrefix(sort, "-") {
		return column + " DESC, source_key", nil
	}
	return column + ", source_key", nil
}

func countFacet(counts map[string]int, value any) {
	if key := str(value); key != "" {
		counts[key]++
	}
}
