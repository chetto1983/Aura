package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testFixturesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "internal", "conversation", "testdata", "bench")
}

func TestLoadFixtures(t *testing.T) {
	fixtures, err := loadFixtures(testFixturesDir(t))
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}
	if len(fixtures) != 4 {
		t.Fatalf("fixtures len = %d, want 4", len(fixtures))
	}
	got := map[string]int{}
	for _, fixture := range fixtures {
		got[fixture.Name] = len(fixture.Messages)
		if fixture.Expected.Keyword == "" {
			t.Fatalf("fixture %s missing keyword", fixture.Name)
		}
	}
	want := map[string]int{
		"long_session":        201,
		"multimodal_visual":   29,
		"short_qa":            21,
		"tool_heavy_research": 71,
	}
	for name, count := range want {
		if got[name] != count {
			t.Fatalf("fixture %s message count = %d, want %d", name, got[name], count)
		}
	}
}

func TestLoadFixtureRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"messages":[{"role":"user","content":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFixture(path); err == nil {
		t.Fatal("loadFixture succeeded for malformed fixture")
	}
}

func TestRunBenchOfflineProducesSchemaAndGate(t *testing.T) {
	models, err := parseModels(defaultModels)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runBench(t.Context(), benchOptions{
		fixturesDir:  testFixturesDir(t),
		models:       models,
		offline:      true,
		thresholdPct: 0.50,
		followup:     true,
	})
	if err != nil {
		t.Fatalf("runBench: %v", err)
	}
	if run.Summary.Rows != 12 {
		t.Fatalf("rows = %d, want 12", run.Summary.Rows)
	}
	if !run.Summary.GatePassed {
		t.Fatalf("gate should pass offline; summary=%+v", run.Summary)
	}
	if run.Summary.BestSavingsPct <= 40 {
		t.Fatalf("best savings = %d, want > 40", run.Summary.BestSavingsPct)
	}
	for _, result := range run.Results {
		if result.Model == "" || result.Fixture == "" || result.PreTokens <= 0 || result.PostTokens <= 0 {
			t.Fatalf("invalid result row: %+v", result)
		}
		if result.RecommendedThresholdPct == 0 {
			t.Fatalf("missing recommendation: %+v", result)
		}
	}
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("run JSON is invalid")
	}
}

func TestWriteRunCreatesArtifact(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "run.json")
	run := runFile{Mode: "offline", Results: []benchResult{{Model: "m", Fixture: "f", PreTokens: 10, PostTokens: 5}}}
	if err := writeRun(out, run); err != nil {
		t.Fatalf("writeRun: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("artifact JSON invalid: %s", raw)
	}
}

func TestRecommendThreshold(t *testing.T) {
	tests := []struct {
		name    string
		pre     int
		window  int
		savings int
		quality bool
		want    int
	}{
		{name: "quality fail", pre: 120000, window: 200000, savings: 10, quality: false, want: 40},
		{name: "late high savings", pre: 180000, window: 200000, savings: 60, quality: true, want: 45},
		{name: "far from threshold", pre: 10000, window: 200000, savings: 0, quality: true, want: 55},
		{name: "default", pre: 120000, window: 200000, savings: 10, quality: true, want: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recommendThreshold(tt.pre, tt.window, tt.savings, tt.quality); got != tt.want {
				t.Fatalf("recommendThreshold() = %d, want %d", got, tt.want)
			}
		})
	}
}
