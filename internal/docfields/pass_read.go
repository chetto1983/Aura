package docfields

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// passBatch bounds the documents one pass reads, so a library indexed all at once is worked
// through over several passes rather than in one long one.
const passBatch = 200

// passagePage bounds the passages one response of the passage read holds.
const passagePage = 256

const documentTextColumns = "source_key, file_name, card, header_pairs, indexed_at"

// The pass marks each document it read with the indexed_at it read, in df_passed_at, and reads
// every document whose mark is missing or older. A watermark on indexed_at assumed documents
// become visible in indexed_at order, and the ingest breaks that: it stamps indexed_at when it
// declares a document and writes it later, concurrently. Measured 2026-10-23 on the lab VM, 8 of
// 14 invoices uploaded together were indexed at .615-.616, became visible after a pass had read
// those at .617-.618, and were never read. A document indexed again carries a newer indexed_at
// than its mark, so it is read again.
const documentsDueWhere = "indexed_at IS NOT NULL AND (df_passed_at IS NULL OR df_passed_at <> indexed_at)"

// hasIndexedDocuments reports whether the identity's ingest has created its document type yet.
func hasIndexedDocuments(ctx context.Context, c *arcadedb.Client) (bool, error) {
	rows, err := c.Query(ctx, "SELECT name FROM schema:types WHERE name = :name",
		map[string]any{"name": arcadedb.IndexedDocumentType})
	if err != nil {
		return false, fmt.Errorf("read the pass state: %w", err)
	}
	return len(rows) > 0, nil
}

// documentsDue reads the documents no pass has read at their current indexed_at, oldest first.
// No index answers it: it scans the type, as the watermark's range did, in half the time
// (2026-10-23, 26.10.1, nothing due: 17.8 ms over 4,000 documents and 72 ms over 40,000, against
// 34.8 ms and 134.5 ms).
func documentsDue(ctx context.Context, c *arcadedb.Client, limit int) ([]DocumentText, error) {
	return readDocuments(ctx, c, documentsDueWhere, map[string]any{}, "indexed_at, source_key", limit)
}

// markPassed marks each document as read at the indexed_at the pass read it at. A document indexed
// again since is left unmarked, so the next pass reads its new version. indexed_at compares as the
// string ArcadeDB returns it, to the millisecond (measured 2026-10-18 on 26.10.1).
func markPassed(ctx context.Context, c *arcadedb.Client, docs []DocumentText) error {
	var script strings.Builder
	params := make(map[string]any, 2*len(docs))
	for i, doc := range docs {
		params[fmt.Sprintf("k%d", i)], params[fmt.Sprintf("a%d", i)] = doc.SourceKey, doc.IndexedAt
		fmt.Fprintf(&script, "UPDATE %s SET df_passed_at = indexed_at WHERE source_key = :k%[2]d AND indexed_at = :a%[2]d;\n",
			arcadedb.IndexedDocumentType, i)
	}
	if _, err := c.Script(ctx, script.String(), params); err != nil {
		return fmt.Errorf("mark the documents passed: %w", err)
	}
	return nil
}

// documentsFrom reads the documents whose key sorts after the given one: the whole library, a
// page at a time.
func documentsFrom(ctx context.Context, c *arcadedb.Client, afterKey string, limit int) ([]DocumentText, error) {
	return readDocuments(ctx, c, "source_key > :from_key", map[string]any{"from_key": afterKey}, "source_key", limit)
}

func readDocuments(ctx context.Context, c *arcadedb.Client, where string, params map[string]any, order string, limit int) ([]DocumentText, error) {
	rows, err := c.Query(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %d",
		documentTextColumns, arcadedb.IndexedDocumentType, where, order, limit), params)
	if err != nil {
		return nil, fmt.Errorf("read indexed documents: %w", err)
	}
	docs := make([]DocumentText, 0, len(rows))
	byKey := make(map[string]int, len(rows))
	for _, row := range rows {
		doc := DocumentText{SourceKey: str(row["source_key"]), FileName: str(row["file_name"]), Card: str(row["card"]),
			IndexedAt: str(row["indexed_at"])}
		for _, pair := range listOf(row["header_pairs"]) {
			doc.HeaderPairs = append(doc.HeaderPairs, str(pair))
		}
		byKey[doc.SourceKey] = len(docs)
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		return nil, nil
	}
	return docs, readPassages(ctx, c, docs, byKey)
}

// readPassages fills each document's text with its passages, in order. No response holds more
// than passagePage passages: the documents are packed into requests by their passage counts, and
// one longer than a page is read alone, a page at a time, until it has the text the pass matches.
// 200 manuals of 600 KB in one response would be about 120 MB, past the client's timeout, and the
// pass would retry that batch for good.
func readPassages(ctx context.Context, c *arcadedb.Client, docs []DocumentText, byKey map[string]int) error {
	keys := make([]string, 0, len(docs))
	for _, doc := range docs {
		keys = append(keys, doc.SourceKey)
	}
	counts, err := passageCounts(ctx, c, keys)
	if err != nil {
		return err
	}
	texts := make([][]string, len(docs))
	sizes := make([]int, len(docs))
	take := func(rows []map[string]any) {
		for _, row := range rows {
			i, ok := byKey[str(row["source_key"])]
			if !ok || sizes[i] >= maxPassText {
				continue
			}
			text := str(row["text"])
			texts[i], sizes[i] = append(texts[i], text), sizes[i]+len(text)+1
		}
	}
	var group []string
	packed := 0
	flush := func() error {
		if len(group) == 0 {
			return nil
		}
		rows, err := c.Query(ctx, "SELECT source_key, text FROM Passage WHERE source_key IN :keys ORDER BY source_key, ordinal",
			map[string]any{"keys": group})
		if err != nil {
			return fmt.Errorf("read passages: %w", err)
		}
		take(rows)
		group, packed = nil, 0
		return nil
	}
	for _, key := range keys {
		n := counts[key]
		if n == 0 {
			continue
		}
		if n > passagePage {
			if err := readLongPassages(ctx, c, key, func() bool { return sizes[byKey[key]] >= maxPassText }, take); err != nil {
				return err
			}
			continue
		}
		if packed+n > passagePage {
			if err := flush(); err != nil {
				return err
			}
		}
		group, packed = append(group, key), packed+n
	}
	if err := flush(); err != nil {
		return err
	}
	for i := range docs {
		docs[i].Text = strings.Join(texts[i], "\n")
	}
	return nil
}

func passageCounts(ctx context.Context, c *arcadedb.Client, keys []string) (map[string]int, error) {
	rows, err := c.Query(ctx, "SELECT source_key, count(*) AS n FROM Passage WHERE source_key IN :keys GROUP BY source_key",
		map[string]any{"keys": keys})
	if err != nil {
		return nil, fmt.Errorf("count passages: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		n, _ := row["n"].(float64)
		counts[str(row["source_key"])] = int(n)
	}
	return counts, nil
}

// readLongPassages reads one document's passages a page at a time, by ordinal, until full says the
// document has the text the pass matches or none is left.
func readLongPassages(ctx context.Context, c *arcadedb.Client, key string, full func() bool, take func([]map[string]any)) error {
	after := any(-1)
	for !full() {
		rows, err := c.Query(ctx, fmt.Sprintf("SELECT source_key, ordinal, text FROM Passage WHERE source_key = :k AND ordinal > :from_ordinal "+
			"ORDER BY ordinal LIMIT %d", passagePage), map[string]any{"k": key, "from_ordinal": after})
		if err != nil {
			return fmt.Errorf("read passages of %s: %w", key, err)
		}
		take(rows)
		if len(rows) < passagePage {
			return nil
		}
		after = rows[len(rows)-1]["ordinal"]
	}
	return nil
}

// passLists reads the identity's labels and custom fields, with the rules compiled.
func (s *Store) passLists(ctx context.Context, identityID string) (passLists, error) {
	c, err := s.client(ctx, identityID)
	if err != nil {
		return passLists{}, err
	}
	rows, err := c.Query(ctx, "SELECT FROM DocumentLabel", nil)
	if err != nil {
		return passLists{}, fmt.Errorf("read labels: %w", err)
	}
	labels := make([]Label, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, decodeLabel(row))
	}
	rows, err = c.Query(ctx, "SELECT id, name_key FROM DocumentCustomField", nil)
	if err != nil {
		return passLists{}, fmt.Errorf("read custom fields: %w", err)
	}
	custom := make(map[string]string, len(rows))
	for _, row := range rows {
		custom[str(row["name_key"])] = str(row["id"])
	}
	return compileLists(labels, custom)
}

// label reads one label by id.
func (s *Store) label(ctx context.Context, identityID, id string) (Label, error) {
	c, err := s.client(ctx, identityID)
	if err != nil {
		return Label{}, err
	}
	rows, err := c.Query(ctx, "SELECT FROM DocumentLabel WHERE id = :id", map[string]any{"id": id})
	if err != nil {
		return Label{}, fmt.Errorf("read label: %w", err)
	}
	if len(rows) == 0 {
		return Label{}, fmt.Errorf("%w: label %s", ErrNotFound, id)
	}
	return decodeLabel(rows[0]), nil
}

func listOf(raw any) []any {
	items, _ := raw.([]any)
	return items
}
