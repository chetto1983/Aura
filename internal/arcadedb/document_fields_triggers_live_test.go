//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"testing"
)

// EnsureDocumentFieldsSchema runs the two passes docfields runs in production: the types and
// indexes, then the triggers that keep their projection on the indexed rows. It reports whether
// the triggers are in place: they need ingest's Passage and IndexedDocument types, and ArcadeDB
// refuses a trigger on a type that does not exist (measured 2026-10-18 on 26.10.1).
func (c *Client) EnsureDocumentFieldsSchema(ctx context.Context) (bool, error) {
	if err := c.EnsureDocumentFieldsTypes(ctx); err != nil {
		return false, err
	}
	return c.EnsureDocumentFieldsTriggers(ctx)
}

// The triggers against what CocoIndex actually writes: a MERGE on the key, then SET n += $props
// (cocoindex 1.0.25 connectors/neo4j/_cypher.py:95-104), over openCypher.
func TestDocumentFieldsTriggersStampCocoIndexWrites(t *testing.T) {
	c, ctx := disposableArcadeClient(t), context.Background()
	if ready, err := c.EnsureDocumentFieldsSchema(ctx); err != nil || ready {
		t.Fatalf("before ingest: ready %v, err %v; want the types without triggers", ready, err)
	}
	mustCommand(t, c, "INSERT INTO DocumentFields SET source_key = 'chat/a.pdf', record_id = 'r-a', type = 'lbl-fattura', "+
		"type_ok = 'lbl-fattura', tags = ['lbl-urgente'], tags_ok = []")
	for _, ddl := range []string{"CREATE VERTEX TYPE Passage IF NOT EXISTS", "CREATE VERTEX TYPE IndexedDocument IF NOT EXISTS"} {
		mustCommand(t, c, ddl)
	}
	mergePassage(t, c, "a#0", "chat/a.pdf", "indexed before the triggers")

	if ready, err := c.EnsureDocumentFieldsSchema(ctx); err != nil || !ready {
		t.Fatalf("after ingest: ready %v, err %v", ready, err)
	}
	assertStamp(t, c, "Passage", "passage_key = 'a#0'", "lbl-fattura", "[lbl-urgente]")

	mergePassage(t, c, "a#1", "chat/a.pdf", "indexed after")
	mergeDocument(t, c, "doc-a", "chat/a.pdf")
	assertStamp(t, c, "Passage", "passage_key = 'a#1'", "lbl-fattura", "[lbl-urgente]")
	assertStamp(t, c, "IndexedDocument", "search_document_id = 'doc-a'", "lbl-fattura", "[lbl-urgente]")
	mergePassage(t, c, "z#0", "chat/unlabelled.pdf", "nobody labelled it")
	assertStamp(t, c, "Passage", "passage_key = 'z#0'", "<nil>", "<nil>")

	mustCommand(t, c, "UPDATE DocumentFields SET type = 'lbl-contratto', tags = [] WHERE source_key = 'chat/a.pdf'")
	assertStamp(t, c, "Passage", "source_key = 'chat/a.pdf'", "lbl-contratto", "[]")
	assertStamp(t, c, "IndexedDocument", "source_key = 'chat/a.pdf'", "lbl-contratto", "[]")

	mergePassage(t, c, "a#1", "chat/a.pdf", "new bytes under the same key")
	assertStamp(t, c, "Passage", "passage_key = 'a#1'", "lbl-contratto", "[]")

	// A rename: Go moves the record first, then the ingest writes the passages under the new key.
	mustCommand(t, c, "UPDATE DocumentFields SET source_key = 'chat/b.pdf' WHERE record_id = 'r-a'")
	mergePassage(t, c, "b#0", "chat/b.pdf", "renamed")
	assertStamp(t, c, "Passage", "passage_key = 'b#0'", "lbl-contratto", "[]")

	mustCommand(t, c, "DELETE FROM DocumentFields WHERE record_id = 'r-a'")
	assertStamp(t, c, "Passage", "passage_key = 'b#0'", "<nil>", "<nil>")
}

// A trigger whose body differs from the current one is replaced, and the records cascade again.
func TestDocumentFieldsTriggersConverge(t *testing.T) {
	c, ctx := disposableArcadeClient(t), context.Background()
	for _, ddl := range []string{"CREATE VERTEX TYPE Passage IF NOT EXISTS", "CREATE VERTEX TYPE IndexedDocument IF NOT EXISTS"} {
		mustCommand(t, c, ddl)
	}
	if _, err := c.EnsureDocumentFieldsSchema(ctx); err != nil {
		t.Fatal(err)
	}
	mustCommand(t, c, "DROP TRIGGER df_fields_update_passage")
	mustCommand(t, c, "CREATE TRIGGER df_fields_update_passage AFTER UPDATE ON TYPE DocumentFields EXECUTE SQL 'SELECT 1'")
	mustCommand(t, c, "INSERT INTO DocumentFields SET source_key = 'chat/c.pdf', record_id = 'r-c', type = 'lbl-x'")
	mergePassage(t, c, "c#0", "chat/c.pdf", "stamped at creation")
	mustCommand(t, c, "UPDATE DocumentFields SET type = 'lbl-y' WHERE record_id = 'r-c'")
	assertStamp(t, c, "Passage", "passage_key = 'c#0'", "lbl-x", "<nil>")

	if ready, err := c.EnsureDocumentFieldsSchema(ctx); err != nil || !ready {
		t.Fatalf("ready %v, err %v", ready, err)
	}
	assertStamp(t, c, "Passage", "passage_key = 'c#0'", "lbl-y", "<nil>")
	rows, err := c.Query(ctx, "SELECT name, actionCode FROM schema:triggers", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, tr := range documentFieldsTriggers() {
		want[tr.name] = tr.code
	}
	if len(rows) != len(want) {
		t.Fatalf("%d triggers, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		if row["actionCode"] != want[fmt.Sprint(row["name"])] {
			t.Errorf("%v kept an old body: %v", row["name"], row["actionCode"])
		}
	}
}

func mustCommand(t *testing.T, c *Client, statement string) {
	t.Helper()
	if _, err := c.Command(context.Background(), statement, nil); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func mergePassage(t *testing.T, c *Client, key, sourceKey, text string) {
	t.Helper()
	if _, err := c.Write(context.Background(), "MERGE (n:Passage {passage_key: $key_0}) SET n += $props",
		map[string]any{"key_0": key, "props": map[string]any{"source_key": sourceKey, "text": text}}); err != nil {
		t.Fatalf("merge passage %s: %v", key, err)
	}
}

func mergeDocument(t *testing.T, c *Client, id, sourceKey string) {
	t.Helper()
	if _, err := c.Write(context.Background(), "MERGE (n:IndexedDocument {search_document_id: $key_0}) SET n += $props",
		map[string]any{"key_0": id, "props": map[string]any{"source_key": sourceKey, "card": "a card"}}); err != nil {
		t.Fatalf("merge document %s: %v", id, err)
	}
}

func assertStamp(t *testing.T, c *Client, typeName, where, wantType, wantTags string) {
	t.Helper()
	rows, err := c.Query(context.Background(), "SELECT df_type, df_tags, df_stamped FROM "+typeName+" WHERE "+where, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s WHERE %s: no rows", typeName, where)
	}
	for _, row := range rows {
		if fmt.Sprint(row["df_type"]) != wantType || fmt.Sprint(row["df_tags"]) != wantTags || row["df_stamped"] != true {
			t.Fatalf("%s WHERE %s: %v, want df_type %s, df_tags %s, stamped", typeName, where, row, wantType, wantTags)
		}
	}
}
