//go:build arcadedb_integration

package arcadedb

import (
	"slices"
	"testing"
)

func TestMentionSchemaRetiresActiveKeyIndex(t *testing.T) {
	client := temporalGraphFixture(t)
	oldIndex := "MENTIONS[@out,@in,fact_key]"
	if _, err := client.Command(t.Context(), "CREATE INDEX IF NOT EXISTS ON MENTIONS (`@out`,`@in`,fact_key) UNIQUE", nil); err != nil {
		t.Fatal(err)
	}
	before, err := client.Query(t.Context(), "SELECT count(*) AS n FROM FACT", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureMemorySchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	schema, err := client.Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range schema.Edges {
		if edge.Name == mentionsEdgeType && slices.Contains(edge.Indexes, oldIndex) {
			t.Fatal("obsolete nullable correction-key index still blocks historical supports")
		}
	}
	after, err := client.Query(t.Context(), "SELECT count(*) AS n FROM FACT", nil)
	if err != nil || before[0]["n"] != after[0]["n"] {
		t.Fatal("schema upgrade changed facts")
	}
}
