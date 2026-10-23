//go:build arcadedb_integration

package docfields

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields/rules"
)

// indexedLibrary is an identity whose database holds ingest's two types, as after a first ingest.
func indexedLibrary(t *testing.T) (*Store, context.Context, string, *arcadedb.Client) {
	t.Helper()
	s, ctx := liveStore(t)
	id := uuid.NewString()
	c, err := s.tenants.For(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		"CREATE VERTEX TYPE Passage IF NOT EXISTS", "CREATE VERTEX TYPE IndexedDocument IF NOT EXISTS",
		"CREATE PROPERTY IndexedDocument.indexed_at IF NOT EXISTS DATETIME",
	} {
		if _, err := c.Command(ctx, ddl, nil); err != nil {
			t.Fatal(err)
		}
	}
	return s, ctx, id, c
}

// docID is the search_document_id a test document is indexed under.
func docID(key string) string { return "doc:" + key }

// indexDocument writes a document and its passage the way the ingest leaves them.
func indexDocument(t *testing.T, c *arcadedb.Client, key, at, text string, pairs ...string) {
	t.Helper()
	if pairs == nil {
		pairs = []string{}
	}
	if _, err := c.Script(context.Background(), "DELETE FROM IndexedDocument WHERE source_key = :k;\n"+
		"INSERT INTO IndexedDocument SET search_document_id = :id, source_key = :k, file_name = :k, card = :k, "+
		"size_bytes = 1024, header_pairs = :pairs, indexed_at = :at",
		map[string]any{"id": docID(key), "k": key, "pairs": pairs, "at": at}); err != nil {
		t.Fatal(err)
	}
	mergePassage(t, c, key+"#0", key)
	if _, err := c.Command(context.Background(), "UPDATE Passage SET text = :text WHERE passage_key = :p",
		map[string]any{"text": text, "p": key + "#0"}); err != nil {
		t.Fatal(err)
	}
}

func officeLabels(t *testing.T, s *Store, ctx context.Context, id string, c *arcadedb.Client) (fattura, contratto Label) {
	t.Helper()
	var err error
	if fattura, err = s.SaveLabel(ctx, id, Label{Kind: KindType, Name: "Fattura", Algorithm: rules.Any, Pattern: "fattura", CaseInsensitive: true}); err != nil {
		t.Fatal(err)
	}
	if contratto, err = s.SaveLabel(ctx, id, Label{Kind: KindType, Name: "Contratto"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLabel(ctx, id, Label{Kind: KindCounterparty, Name: "ACME SpA"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Command(ctx, "INSERT INTO DocumentCustomField SET id = 'cf-stato', name = 'Stato', name_key = 'stato'", nil); err != nil {
		t.Fatal(err)
	}
	return fattura, contratto
}

func TestPassRunsOverEachNewDocumentOnce(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	if result, err := s.RunPass(ctx, id); err != nil || result.Documents != 0 {
		t.Fatalf("a library with nothing indexed: %+v, %v", result, err)
	}
	fattura, _ := officeLabels(t, s, ctx, id, c)
	indexDocument(t, c, "chat/f1.xlsx", "2026-10-18 10:00:00.000", "Fattura per servizi",
		"Fattura n.: 2026_001", "Cliente: ACME SpA", "Data emissione: 12/01/2026", "Stato: pagata")
	result, err := s.RunPass(ctx, id)
	if err != nil || result.Documents != 1 || result.Suggested != 4 {
		t.Fatalf("first pass: %+v, %v", result, err)
	}
	f, err := s.Record(ctx, id, "chat/f1.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(f.value(FieldType).ValueKey, " ", f.value(FieldDocumentDate).ValueKey, " ", f.value("custom:cf-stato").ValueKey)
	if want := fattura.ID + " 2026-01-12 pagata"; got != want || f.value(FieldCounterparty).SetBy != SetByExtractor {
		t.Fatalf("fields = %q (%+v), want %q", got, f.Values, want)
	}
	if result, err := s.RunPass(ctx, id); err != nil || result.Documents != 0 {
		t.Fatalf("a second pass over nothing new: %+v, %v", result, err)
	}
	indexDocument(t, c, "chat/f2.xlsx", "2026-10-18 10:00:00.001", "una nota")
	if result, err := s.RunPass(ctx, id); err != nil || result.Documents != 1 || result.Suggested != 0 {
		t.Fatalf("a pass over one new document: %+v, %v", result, err)
	}
}

// A batch can end inside a group of documents indexed in the same millisecond, and the next pass
// must still read the rest of the group. Measured 2026-10-18 on the dev stack: a re-ingest wrote
// three documents at one instant across a batch boundary, and a watermark on indexed_at alone
// skipped them for good, an invoice among them.
func TestPassReadsEveryDocumentOfAnInstantABatchSplits(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	const total = passBatch + 2
	var script strings.Builder
	params := map[string]any{"at": "2026-10-18 10:00:00.000"}
	for i := range total {
		key := fmt.Sprintf("chat/same-instant-%03d.txt", i)
		params[fmt.Sprintf("k%d", i)] = key
		fmt.Fprintf(&script, "INSERT INTO IndexedDocument SET search_document_id = :k%[1]d, source_key = :k%[1]d, "+
			"file_name = :k%[1]d, card = '', header_pairs = [], indexed_at = :at;\n", i)
	}
	if _, err := c.Script(ctx, script.String(), params); err != nil {
		t.Fatal(err)
	}
	read := 0
	for range 3 {
		result, err := s.RunPass(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		read += result.Documents
	}
	if read != total {
		t.Fatalf("the passes read %d documents, want every one of the %d indexed at one instant", read, total)
	}
}

// The ingest writes a library concurrently, so a document can become visible after one indexed later
// than it: measured 2026-10-23 on the lab VM, 8 of 14 invoices uploaded together were indexed at
// .615-.616, became visible after a pass had read those at .617-.618, and no pass read them again.
func TestPassReadsADocumentThatBecameVisibleLate(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	indexDocument(t, c, "chat/visible-first.xlsx", "2026-10-23 06:37:41.618", "Fattura", "Cliente: ACME SpA")
	if _, err := s.RunPass(ctx, id); err != nil {
		t.Fatal(err)
	}
	indexDocument(t, c, "chat/visible-late.xlsx", "2026-10-23 06:37:41.615", "Fattura", "Cliente: Rossi SRL")
	if _, err := s.RunPass(ctx, id); err != nil {
		t.Fatal(err)
	}
	late, err := s.Record(ctx, id, "chat/visible-late.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if v := late.value(FieldCounterparty); v.ValueKey != proposedPrefix+"Rossi SRL" {
		t.Fatalf("counterparty = %+v, want the extractor's Rossi SRL", v)
	}
}

// The operator removes a suggestion, then the document is indexed again and the rule saved again:
// the value comes back from neither (Review Focus 4).
func TestPassNeverResuggestsRejected(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	fattura, _ := officeLabels(t, s, ctx, id, c)
	indexDocument(t, c, "chat/f1.xlsx", "2026-10-18 10:00:00.000", "Fattura per servizi", "Data emissione: 2026-01-12")
	if _, err := s.RunPass(ctx, id); err != nil {
		t.Fatal(err)
	}
	f, err := s.Record(ctx, id, "chat/f1.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	removed := Diff{Remove: []ValueRef{{Field: FieldType, ValueKey: fattura.ID}, {Field: FieldDocumentDate, ValueKey: "2026-01-12"}}}
	if err := s.ApplyDiff(ctx, id, []string{f.RecordID}, removed, SetByOperator); err != nil {
		t.Fatal(err)
	}
	indexDocument(t, c, "chat/f1.xlsx", "2026-10-18 11:00:00.000", "Fattura per servizi, rivista", "Data emissione: 2026-01-12")
	if result, err := s.RunPass(ctx, id); err != nil || result.Documents != 1 || result.Suggested != 0 {
		t.Fatalf("pass after the rejection: %+v, %v", result, err)
	}
	if result, err := s.RunRule(ctx, id, fattura.ID); err != nil || result.Suggested != 0 {
		t.Fatalf("rule after the rejection: %+v, %v", result, err)
	}
	if f, _ := s.Record(ctx, id, "chat/f1.xlsx"); len(f.Values) != 0 {
		t.Fatalf("values came back: %+v", f.Values)
	}
}

func TestPassNeverTouchesOperatorValue(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	fattura, contratto := officeLabels(t, s, ctx, id, c)
	indexDocument(t, c, "chat/c1.pdf", "2026-10-18 10:00:00.000", "Contratto, con la prima fattura allegata")
	f, err := s.Record(ctx, id, "chat/c1.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDiff(ctx, id, []string{f.RecordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: contratto.ID}}}, SetByOperator); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunPass(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunRule(ctx, id, fattura.ID); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Record(ctx, id, "chat/c1.pdf"); f.value(FieldType).ValueKey != contratto.ID || f.value(FieldType).SetBy != SetByOperator {
		t.Fatalf("type = %+v, want the operator's Contratto", f.value(FieldType))
	}
}

// Writes to one identity's records are serialized: ten concurrent edits of one document all land.
func TestConcurrentWritesKeepEveryChange(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	f, err := s.Record(ctx, id, "chat/busy.pdf")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := range 10 {
		wg.Go(func() {
			tag := Value{Field: FieldTag, ValueKey: fmt.Sprintf("lbl-%d", i), SetBy: "rule:lbl"}
			if i%2 == 0 {
				errs <- s.ApplyDiff(ctx, id, []string{f.RecordID}, Diff{Set: []Value{tag}}, SetByOperator)
				return
			}
			_, err := s.Suggest(ctx, id, f.RecordID, []Value{tag})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Record(ctx, id, "chat/busy.pdf")
	if tags := got.keys(FieldTag); len(tags) != 10 || !slices.Contains(tags, "lbl-9") {
		t.Fatalf("tags = %v, want all ten", tags)
	}
}
