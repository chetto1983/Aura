package temporal_test

import (
	"math"
	"reflect"
	"testing"
)

func TestCommunityMetricCounterexamples(t *testing.T) {
	client, _ := algorithmFixture(t)
	cut, err := client.Query(t.Context(), "SELECT count(*) AS n FROM FACT WHERE outV().community <> inV().community", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.Read(t.Context(), "CALL algo.conductance('community','FACT') YIELD community,boundaryEdges,conductance RETURN community,boundaryEdges,conductance", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cut[0]["n"] != float64(1) {
		t.Fatal("counterexample must contain one inter-community edge")
	}
	for _, row := range rows {
		if row["community"] == float64(3) {
			continue
		}
		t.Logf("conductance counterexample: community=%v actual_cut=1 expected=1/7 native_cut=%v native_conductance=%v", row["community"], row["boundaryEdges"], row["conductance"])
		// Characterize the observed engine defect; this is not a correctness pass.
		if row["boundaryEdges"] != float64(0) || row["conductance"] != float64(0) {
			t.Fatal("conductance behavior changed; reassess the recorded defect")
		}
	}
	command(t, client, "UPDATE Entity SET community=1", nil)
	rows, err = client.Read(t.Context(), "CALL algo.modularityScore('community','FACT') YIELD modularity RETURN modularity", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("single-community modularity: mathematical_oracle=0 native=%v", rows[0]["modularity"])
	if rows[0]["modularity"] != 0.75 {
		t.Fatal("modularity behavior changed; reassess the recorded normalization defect")
	}
}

func TestCommunityAndEmbeddingRepeatability(t *testing.T) {
	client, names := algorithmFixture(t)
	for _, tc := range algorithmCases() {
		if tc.name != "leiden" && tc.name != "louvain" && tc.name != "labelpropagation" && tc.name != "fastrp" && tc.name != "graphsage" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			query := "CALL " + tc.call + " YIELD " + tc.fields + " RETURN " + tc.fields
			var previous any
			for i := 0; i < 3; i++ {
				rows, err := client.Read(t.Context(), query, nil)
				if err != nil {
					t.Fatal(err)
				}
				normalized := normalizeResult(rows, names)
				if i > 0 && !reflect.DeepEqual(previous, normalized) {
					t.Fatal("fixed-input repeat changed result")
				}
				previous = normalized
			}
			t.Log("three identical fixed-input runs")
			if tc.name != "fastrp" && tc.name != "graphsage" {
				return
			}
			vectors := map[string][]any{}
			for _, raw := range previous.([]any) {
				row := raw.(map[string]any)
				vectors[row["node"].(string)] = row["embedding"].([]any)
			}
			for _, other := range []string{"B", "D", "Isolated"} {
				x, y := vectors["A"], vectors[other]
				dot, nx, ny := 0.0, 0.0, 0.0
				for i := range x {
					a, b := x[i].(float64), y[i].(float64)
					dot += a * b
					nx += a * a
					ny += b * b
				}
				if nx == 0 || ny == 0 {
					t.Logf("cosine A/%s undefined: zero vector", other)
					continue
				}
				t.Logf("cosine A/%s=%g", other, dot/math.Sqrt(nx*ny))
			}
		})
	}
}
