package temporal_test

import (
	"fmt"
	"testing"
)

func TestStructuralSignalsDependOnTemporalProjection(t *testing.T) {
	client := fixture(t)
	keys := keysAt(t, client, "2026-07-01T00:00:00Z")
	rows, err := client.Query(t.Context(), "SELECT @rid AS rid, name FROM Entity", nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, row := range rows {
		names[row["rid"].(string)] = row["name"].(string)
	}
	// This materialized edge type exists only in the disposable experiment DB.
	// It measures projection effects without claiming a production snapshot API.
	command(t, client, "CREATE EDGE TYPE VALID_FACT", nil)
	facts, err := client.Query(t.Context(), "SELECT outV().name AS subject, inV().name AS object FROM FACT WHERE fact_key IN :keys", map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		command(t, client, "CREATE EDGE VALID_FACT FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target)", map[string]any{"source": fact["subject"], "target": fact["object"]})
	}
	for _, relations := range []string{"FACT", "VALID_FACT"} {
		rows, err := client.Read(t.Context(), fmt.Sprintf("CALL algo.kcore('%s') YIELD node, coreNumber RETURN node, coreNumber", relations), nil)
		if err != nil {
			t.Fatal(err)
		}
		core := map[string]float64{}
		for _, row := range rows {
			core[names[row["node"].(string)]] = row["coreNumber"].(float64)
		}
		rows, err = client.Read(t.Context(), fmt.Sprintf("CALL algo.articulationPoints('%s') YIELD node RETURN node", relations), nil)
		if err != nil {
			t.Fatal(err)
		}
		cuts := map[string]bool{}
		for _, row := range rows {
			cuts[names[row["node"].(string)]] = true
		}
		rows, err = client.Read(t.Context(), fmt.Sprintf("CALL algo.triangleCount('%s') YIELD node, triangles RETURN node, triangles", relations), nil)
		if err != nil {
			t.Fatal(err)
		}
		triangles := map[string]float64{}
		for _, row := range rows {
			triangles[names[row["node"].(string)]] = row["triangles"].(float64)
		}
		t.Logf("relations=%s core=%v articulation=%v triangles=%v", relations, core, cuts, triangles)
		if relations == "FACT" {
			if core["Start"] != 2 || core["Middle"] != 2 || core["Target"] != 2 || triangles["Middle"] != 1 {
				t.Fatal("stored topology differs from expected historical triangles")
			}
		} else {
			if core["Start"] != 1 || core["Middle"] != 1 || core["Target"] != 1 || triangles["Middle"] != 0 || !cuts["Middle"] || !cuts["Target"] {
				t.Fatal("admissible topology differs from expected path with low-core connectors")
			}
		}
	}
}
