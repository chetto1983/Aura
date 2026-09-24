package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

func fakeDoctorEmbeddingReports(t *testing.T, space embeddings.Space, reports []arcadedb.TenantSpaceReport) {
	t.Helper()
	old := doctorEmbeddingReports
	t.Cleanup(func() { doctorEmbeddingReports = old })
	doctorEmbeddingReports = func(context.Context, *config.Config) (embeddings.Space, []arcadedb.TenantSpaceReport, error) {
		return space, reports, nil
	}
}

func family(name string, open bool, other ...int) arcadedb.FamilyState {
	state := arcadedb.FamilyState{Family: name, Open: open}
	for _, n := range other {
		state.Types = append(state.Types, arcadedb.TypeTally{OtherSpace: n})
	}
	return state
}

// Spec §10: doctor names every tenant and family whose gate is closed.
func TestDoctorEmbeddingSpaceNamesEveryClosedGate(t *testing.T) {
	fakeDoctorEmbeddingReports(t, embeddings.Space{ID: "es1-a", Label: "local gemma"}, []arcadedb.TenantSpaceReport{
		{IdentityID: "id-1", Families: []arcadedb.FamilyState{family("memory", false, 2, 1), family("documents", true)}},
		{IdentityID: "id-2", Families: []arcadedb.FamilyState{family("memory", true), family("documents", false, 4)},
			StuckDocuments: []arcadedb.StuckDocument{{FileName: "scan.png"}, {FileName: "clip.mp3"}}},
	})
	_, err := doctorProbeEmbeddingSpace(context.Background(), &config.Config{})
	if err == nil {
		t.Fatal("two closed gates passed as healthy")
	}
	for _, want := range []string{"2 gate(s) closed", "id-1 memory (3 vectors in another space)",
		"id-2 documents (4 vectors in another space: scan.png, clip.mp3)"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("detail %q lacks %q", err, want)
		}
	}
}

func TestDoctorEmbeddingSpacePassesWhenEveryGateIsOpen(t *testing.T) {
	fakeDoctorEmbeddingReports(t, embeddings.Space{ID: "es1-a", Label: "local gemma"}, []arcadedb.TenantSpaceReport{
		{IdentityID: "id-1", Families: []arcadedb.FamilyState{family("memory", true), family("documents", true)}},
	})
	detail, err := doctorProbeEmbeddingSpace(context.Background(), &config.Config{})
	if err != nil || detail != "1 tenant(s) dense in space es1-a (local gemma)" {
		t.Fatalf("detail %q err %v", detail, err)
	}
}

func TestDoctorEmbeddingSpaceWithoutAMemoryServerIsNotConfigured(t *testing.T) {
	detail, err := defaultDoctorProbeEmbeddingSpace(context.Background(), &config.Config{})
	if err != nil || !strings.Contains(detail, "not configured") {
		t.Fatalf("detail %q err %v", detail, err)
	}
}

// A closed gate is a state the pass is already fixing, not an outage: doctor warns and still
// exits 0.
func TestDoctorWarnsOnAClosedGateWithoutFailing(t *testing.T) {
	installDoctorFakeProbes(t)
	fakeDoctorEmbeddingReports(t, embeddings.Space{ID: "es1-a"}, []arcadedb.TenantSpaceReport{
		{IdentityID: "id-1", Families: []arcadedb.FamilyState{family("memory", false, 1), family("documents", true)}},
	})
	doctorProbeEmbeddingSpace = defaultDoctorProbeEmbeddingSpace
	var out bytes.Buffer
	code := runDoctorWithConfig(context.Background(), &out, &config.Config{})
	if code != 0 || !strings.Contains(out.String(), "WARN embedding_space: 1 gate(s) closed") ||
		!strings.Contains(out.String(), "status: OK") {
		t.Fatalf("exit %d output:\n%s", code, out.String())
	}
}
