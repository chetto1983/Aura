//go:build arcadedb_integration

// Live-ArcadeDB tests for the document fields store: what only the database can prove. Each
// identity gets its own disposable database, as production gives each identity its own, so
// isolation is the server's and not a WHERE clause.
//
//	go test -tags arcadedb_integration -run TestDocfields ./internal/docfields/
package docfields

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// disposableTenants resolves each identity to a database created for the test and dropped after.
type disposableTenants struct {
	t       *testing.T
	admin   *arcadedb.Client
	mu      sync.Mutex
	clients map[string]*arcadedb.Client
}

// arcadeURLOrSkip returns ARCADEDB_URL, skipping locally without it and failing under CI.
func arcadeURLOrSkip(t *testing.T) string {
	t.Helper()
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	return base
}

func newDisposableTenants(t *testing.T) *disposableTenants {
	t.Helper()
	base := arcadeURLOrSkip(t)
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: "unused", User: envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD")})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	return &disposableTenants{t: t, admin: admin, clients: map[string]*arcadedb.Client{}}
}

func (d *disposableTenants) For(ctx context.Context, identityID string) (*arcadedb.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.clients[identityID]; ok {
		return c, nil
	}
	database := fmt.Sprintf("docfields_it_%d", time.Now().UnixNano())
	if _, err := d.admin.CreateDatabase(ctx, database); err != nil {
		return nil, err
	}
	d.t.Cleanup(func() {
		if _, err := d.admin.DropDatabase(context.Background(), database); err != nil {
			d.t.Errorf("drop %s: %v", database, err)
		}
	})
	c, err := arcadedb.New(arcadedb.Config{BaseURL: os.Getenv("ARCADEDB_URL"), Database: database,
		User: envOr("ARCADEDB_USER", "root"), Password: os.Getenv("ARCADEDB_PASSWORD")})
	if err != nil {
		return nil, err
	}
	d.clients[identityID] = c
	return c, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func liveStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	return New(newDisposableTenants(t)), context.Background()
}

func TestDocfieldsOperatorWins(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	rec, err := s.Record(ctx, id, "chat/a.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDiff(ctx, id, []string{rec.RecordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: "lbl-fattura"}}}, SetByOperator); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Suggest(ctx, id, rec.RecordID, []Value{{Field: FieldType, ValueKey: "lbl-contratto", SetBy: SetByExtractor}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Record(ctx, id, "chat/a.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if v := got.value(FieldType); v.ValueKey != "lbl-fattura" || v.SetBy != SetByOperator {
		t.Fatalf("type = %+v, want the operator's lbl-fattura", v)
	}
}

func TestDocfieldsRejectionSticks(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	rec, err := s.Record(ctx, id, "chat/b.pdf")
	if err != nil {
		t.Fatal(err)
	}
	urgent := Value{Field: FieldTag, ValueKey: "lbl-urgente", SetBy: "rule:lbl-urgente", Evidence: "matched 'urgente'"}
	if n, err := s.Suggest(ctx, id, rec.RecordID, []Value{urgent}); err != nil || n != 1 {
		t.Fatalf("first suggestion: added %d, err %v", n, err)
	}
	if err := s.ApplyDiff(ctx, id, []string{rec.RecordID}, Diff{Remove: []ValueRef{urgent.ref()}}, SetByOperator); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Suggest(ctx, id, rec.RecordID, []Value{urgent}); err != nil || n != 0 {
		t.Fatalf("a rejected value came back: added %d, err %v", n, err)
	}
	got, err := s.Record(ctx, id, "chat/b.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 0 || len(got.Rejections) != 1 {
		t.Fatalf("fields = %+v, want no value and one rejection", got)
	}
}

// A rename onto a key that already has a record: the moved record replaces the target's, and the
// replaced record's values end with a removed event (Review Focus 3).
func TestDocfieldsMoveOntoExistingKey(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	moved, err := s.Record(ctx, id, "chat/old.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.Record(ctx, id, "chat/new.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	set := func(recordID, key string) {
		t.Helper()
		if err := s.ApplyDiff(ctx, id, []string{recordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: key}}}, SetByOperator); err != nil {
			t.Fatal(err)
		}
	}
	set(moved.RecordID, "lbl-fattura")
	set(target.RecordID, "lbl-contratto")
	if err := s.Move(ctx, id, "chat/old.xlsx", "chat/new.xlsx"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Record(ctx, id, "chat/new.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if got.RecordID != moved.RecordID || got.value(FieldType).ValueKey != "lbl-fattura" {
		t.Fatalf("new key holds %+v, want the moved record with lbl-fattura", got)
	}
	history, err := s.History(ctx, id, target.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 || history[len(history)-1].Action != ActionRemoved {
		t.Fatalf("replaced record's history = %+v, want it to end with removed", history)
	}
	movedHistory, err := s.History(ctx, id, moved.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if last := movedHistory[len(movedHistory)-1]; last.Action != ActionMoved {
		t.Fatalf("moved record's last event = %+v, want moved", last)
	}
}

// A second identity has its own database: the first identity's record is not there to change.
func TestDocfieldsIsolation(t *testing.T) {
	s, ctx := liveStore(t)
	first, second := uuid.NewString(), uuid.NewString()
	rec, err := s.Record(ctx, first, "chat/private.pdf")
	if err != nil {
		t.Fatal(err)
	}
	err = s.ApplyDiff(ctx, second, []string{rec.RecordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: "lbl-x"}}}, SetByOperator)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("second identity changing the first's record: err = %v, want ErrNotFound", err)
	}
	if labels, err := s.Labels(ctx, second, KindType); err != nil || len(labels) != 0 {
		t.Fatalf("second identity's labels = %v, err %v", labels, err)
	}
}

// Names match case-insensitively and exactly after trimming, unique per identity and kind.
func TestDocfieldsNameCaseInsensitiveUnique(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	if _, err := s.SaveLabel(ctx, id, Label{Kind: KindCounterparty, Name: "Gialli Import"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLabel(ctx, id, Label{Kind: KindCounterparty, Name: "  GIALLI import "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate name: err = %v, want ErrInvalid", err)
	}
	if _, err := s.SaveLabel(ctx, id, Label{Kind: KindTag, Name: "gialli import"}); err != nil {
		t.Fatalf("the same name on another kind: %v", err)
	}
	if _, err := s.SaveLabel(ctx, id, Label{Kind: KindTag, Name: "urgente", Algorithm: "regex", Pattern: "urgent("}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid regex: err = %v, want ErrInvalid", err)
	}
}

// A label lists by name, updates in place, and an unknown id is not found on update or delete.
func TestDocfieldsLabelLifecycle(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	b, err := s.SaveLabel(ctx, id, Label{Kind: KindType, Name: "Fattura"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLabel(ctx, id, Label{Kind: KindType, Name: "Contratto", Algorithm: "any", Pattern: "contratto accordo",
		CaseInsensitive: true}); err != nil {
		t.Fatal(err)
	}
	b.Name, b.Algorithm, b.Pattern = "Fattura passiva", "regex", `fattura\s+n`
	if _, err := s.SaveLabel(ctx, id, b); err != nil {
		t.Fatal(err)
	}
	labels, err := s.Labels(ctx, id, KindType)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 || labels[0].Name != "Contratto" || !labels[0].CaseInsensitive || labels[1] != b {
		t.Fatalf("labels = %+v, want Contratto then the updated %+v", labels, b)
	}
	if _, err := s.SaveLabel(ctx, id, Label{ID: uuid.NewString(), Kind: KindType, Name: "Ghost"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of an unknown id: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteLabel(ctx, id, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLabel(ctx, id, b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
}

// The projection columns the triggers copy: the _ok columns carry only what the operator set or
// accepted, until a review accepts the rest.
func TestDocfieldsProjectionAndReview(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	rec, err := s.Record(ctx, id, "chat/c.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDiff(ctx, id, []string{rec.RecordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: "lbl-fattura"}}}, SetByOperator); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Suggest(ctx, id, rec.RecordID, []Value{
		{Field: FieldTag, ValueKey: "lbl-urgente", SetBy: "rule:lbl-urgente"},
		{Field: FieldDocumentDate, ValueKey: "2026-03-01", SetBy: SetByExtractor},
	}); err != nil {
		t.Fatal(err)
	}
	row := projectionRow(t, s, ctx, id, rec.RecordID)
	if row["type_ok"] != "lbl-fattura" || row["document_date"] != "2026-03-01" || row["document_date_ok"] != nil ||
		fmt.Sprint(row["tags"]) != "[lbl-urgente]" || fmt.Sprint(row["tags_ok"]) != "[]" {
		t.Fatalf("projection before review = %v", row)
	}
	if err := s.MarkReviewed(ctx, id, []string{rec.RecordID}); err != nil {
		t.Fatal(err)
	}
	row = projectionRow(t, s, ctx, id, rec.RecordID)
	if row["document_date_ok"] != "2026-03-01" || fmt.Sprint(row["tags_ok"]) != "[lbl-urgente]" || row["reviewed_by"] != SetByOperator {
		t.Fatalf("projection after review = %v", row)
	}
	got, err := s.Record(ctx, id, "chat/c.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewedAt == nil || !got.value(FieldDocumentDate).Accepted || got.value(FieldDocumentDate).SetBy != SetByExtractor {
		t.Fatalf("reviewed record = %+v, want the date accepted and still the extractor's", got)
	}
}

func projectionRow(t *testing.T, s *Store, ctx context.Context, identityID, recordID string) map[string]any {
	t.Helper()
	c, err := s.client(ctx, identityID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := c.Query(ctx, "SELECT type_ok, document_date, document_date_ok, tags, tags_ok, reviewed_by FROM DocumentFields "+
		"WHERE record_id = :r", map[string]any{"r": recordID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("projection row: %v, %d rows", err, len(rows))
	}
	return rows[0]
}

// A copy holds the source's values as copied and unreviewed, under its own record; the source
// keeps its own.
func TestDocfieldsCopyRecord(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	src, err := s.Record(ctx, id, "chat/orig.docx")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDiff(ctx, id, []string{src.RecordID}, Diff{Set: []Value{{Field: FieldCounterparty, ValueKey: "lbl-gialli"}}}, SetByOperator); err != nil {
		t.Fatal(err)
	}
	if err := s.CopyRecord(ctx, id, "chat/orig.docx", "chat/copia.docx"); err != nil {
		t.Fatal(err)
	}
	dup, err := s.Record(ctx, id, "chat/copia.docx")
	if err != nil {
		t.Fatal(err)
	}
	v := dup.value(FieldCounterparty)
	if dup.RecordID == src.RecordID || v.ValueKey != "lbl-gialli" || v.SetBy != SetByCopied || v.Accepted || v.Evidence != "copied from chat/orig.docx" {
		t.Fatalf("copy = %+v, want its own record with lbl-gialli copied and unreviewed", dup)
	}
	if orig, err := s.Record(ctx, id, "chat/orig.docx"); err != nil || orig.value(FieldCounterparty).SetBy != SetByOperator {
		t.Fatalf("source after the copy = %+v, err %v", orig, err)
	}
	history, err := s.History(ctx, id, dup.RecordID)
	if err != nil || len(history) != 1 || history[0].Actor != SetByCopied || history[0].After == nil {
		t.Fatalf("copy history = %+v, err %v", history, err)
	}
	if err := s.Move(ctx, id, "chat/missing.docx", "chat/elsewhere.docx"); err != nil {
		t.Fatalf("moving a document nobody labelled: %v", err)
	}
}

// The store's writes reach the passages the search scans, through the triggers: the operator's
// values at once, a suggestion only once reviewed, and the fields follow a rename, leaving the old
// key's rows, which the ingest drops only at its next scan, with none.
func TestDocfieldsWritesReachThePassages(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	c, err := s.tenants.For(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{"CREATE VERTEX TYPE Passage IF NOT EXISTS", "CREATE VERTEX TYPE IndexedDocument IF NOT EXISTS"} {
		if _, err := c.Command(ctx, ddl, nil); err != nil {
			t.Fatal(err)
		}
	}
	mergePassage(t, c, "d#0", "chat/d.pdf")
	if _, err := c.Command(ctx, "INSERT INTO IndexedDocument SET source_key = 'chat/d.pdf'", nil); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Record(ctx, id, "chat/d.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDiff(ctx, id, []string{rec.RecordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: "lbl-fattura"}}}, SetByOperator); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Suggest(ctx, id, rec.RecordID, []Value{{Field: FieldTag, ValueKey: "lbl-urgente", SetBy: "rule:lbl-urgente"}}); err != nil {
		t.Fatal(err)
	}
	assertPassage(t, c, "d#0", "lbl-fattura [lbl-urgente] []")
	if err := s.MarkReviewed(ctx, id, []string{rec.RecordID}); err != nil {
		t.Fatal(err)
	}
	assertPassage(t, c, "d#0", "lbl-fattura [lbl-urgente] [lbl-urgente]")
	if err := s.Move(ctx, id, "chat/d.pdf", "chat/e.pdf"); err != nil {
		t.Fatal(err)
	}
	mergePassage(t, c, "e#0", "chat/e.pdf")
	assertPassage(t, c, "e#0", "lbl-fattura [lbl-urgente] [lbl-urgente]")
	assertPassage(t, c, "d#0", "<nil> <nil> <nil>")
	rows, err := c.Query(ctx, "SELECT df_type FROM IndexedDocument WHERE source_key = 'chat/d.pdf'", nil)
	if err != nil || len(rows) != 1 || rows[0]["df_type"] != nil {
		t.Fatalf("the old key's document = %v (err %v), want no type", rows, err)
	}
}

// mergePassage writes a passage the way CocoIndex does (connectors/neo4j/_cypher.py:95-104).
func mergePassage(t *testing.T, c *arcadedb.Client, key, sourceKey string) {
	t.Helper()
	if _, err := c.Write(context.Background(), "MERGE (n:Passage {passage_key: $key_0}) SET n += $props",
		map[string]any{"key_0": key, "props": map[string]any{"source_key": sourceKey, "text": "passage " + key}}); err != nil {
		t.Fatalf("merge passage %s: %v", key, err)
	}
}

func assertPassage(t *testing.T, c *arcadedb.Client, key, want string) {
	t.Helper()
	rows, err := c.Query(context.Background(), "SELECT df_type, df_tags, df_tags_ok FROM Passage WHERE passage_key = :k",
		map[string]any{"k": key})
	if err != nil || len(rows) != 1 {
		t.Fatalf("passage %s: %v, %d rows", key, err, len(rows))
	}
	if got := fmt.Sprint(rows[0]["df_type"], " ", rows[0]["df_tags"], " ", rows[0]["df_tags_ok"]); got != want {
		t.Fatalf("passage %s carries %q, want %q", key, got, want)
	}
}

// A filtered search resolves names against the identity's own lists, and counts nothing in a
// library nothing has been indexed into yet.
func TestDocfieldsResolveAndAdmitBeforeAnyIngest(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	acme, err := s.SaveLabel(ctx, id, Label{Kind: KindCounterparty, Name: "ACME SpA"})
	if err != nil {
		t.Fatal(err)
	}
	ids, unknown, err := s.ResolveNames(ctx, id, KindCounterparty, []string{" acme spa ", "Acme"})
	if err != nil || !slices.Equal(ids, []string{acme.ID}) || !slices.Equal(unknown["Acme"], []string{"ACME SpA"}) {
		t.Fatalf("ids %v, unknown %v, err %v", ids, unknown, err)
	}
	admissions, err := s.Admissions(ctx, id, arcadedb.FieldFilter{Counterparties: ids, IncludeUnreviewed: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []arcadedb.FieldAdmission{{Dimension: arcadedb.FieldCounterparty}}; !slices.Equal(admissions, want) {
		t.Fatalf("admissions = %+v, want %+v", admissions, want)
	}
}

// A file with no fields moved or copied over one that has them: the bytes under the key are no
// longer the labelled document's, so its record goes, the values ending with a removed event. A
// deleted file's record goes the same way, so a file uploaded later under that name starts empty.
func TestDocfieldsARecordGoesWithItsBytes(t *testing.T) {
	s, ctx := liveStore(t)
	id := uuid.NewString()
	steps := map[string]func(key string) error{
		"move":   func(key string) error { return s.Move(ctx, id, "chat/unlabelled.pdf", key) },
		"copy":   func(key string) error { return s.CopyRecord(ctx, id, "chat/unlabelled.pdf", key) },
		"delete": func(key string) error { return s.Remove(ctx, id, key) },
	}
	for name, step := range steps {
		key := "chat/" + name + ".pdf"
		held, err := s.Record(ctx, id, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyDiff(ctx, id, []string{held.RecordID}, Diff{Set: []Value{{Field: FieldType, ValueKey: "lbl-fattura"}}}, SetByOperator); err != nil {
			t.Fatal(err)
		}
		if err := step(key); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if now, err := s.Record(ctx, id, key); err != nil || now.RecordID == held.RecordID || len(now.Values) != 0 {
			t.Fatalf("%s: %s holds %+v (err %v), want a new empty record", name, key, now, err)
		}
		history, err := s.History(ctx, id, held.RecordID)
		if err != nil || history[len(history)-1].Action != ActionRemoved {
			t.Fatalf("%s: the replaced record's history = %+v (err %v), want it to end with removed", name, history, err)
		}
	}
	if err := s.Remove(ctx, id, "chat/never-labelled.pdf"); err != nil {
		t.Fatalf("removing a document nobody labelled: %v", err)
	}
}
