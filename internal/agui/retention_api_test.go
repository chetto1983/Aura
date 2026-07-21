package agui

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"
)

func TestOwnerExporterManifestIsVersionedDeterministicAndVerified(t *testing.T) {
	source := &fakeOwnerExportSource{
		snapshot: ExportSnapshot{
			IdentitySnapshotID: "identity-snapshot", ConversationSnapshotID: "conversation-snapshot",
			ConversationJSON: []byte(`{"turns":[{"role":"user","content":"ciao"}]}`),
			Artifacts: []ExportArtifact{
				{ID: "b-id", Filename: "zeta.txt", Size: 4},
				{ID: "a-id", Filename: "caffè.txt", Size: 5},
			},
		},
		bodies: map[string][]byte{"a-id": []byte("hello"), "b-id": []byte("zeta")},
	}
	destination := NewMemoryExportDestination(1 << 20)
	exporter := &OwnerExporter{
		Source: source, Destination: destination, AuraVersion: "v1.2.3", PolicyVersion: "retention-v1",
		Now: func() time.Time { return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) },
	}
	result, err := exporter.Export(context.Background(), "owner", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.ManifestVersion != ManifestVersion || result.Manifest.AuraVersion != "v1.2.3" || result.Manifest.PolicyVersion != "retention-v1" {
		t.Fatalf("manifest versions = %+v", result.Manifest)
	}
	paths := make([]string, 0, len(result.Manifest.Entries))
	for _, entry := range result.Manifest.Entries {
		if len(entry.SHA256) != 64 || entry.Size < 0 {
			t.Fatalf("invalid manifest entry %+v", entry)
		}
		paths = append(paths, entry.Path)
	}
	if !slices.IsSorted(paths) || !slices.Contains(paths, "assets/a-id/caffè.txt") || !slices.Contains(paths, "conversation.json") {
		t.Fatalf("manifest paths = %v", paths)
	}
	rc, err := destination.Open(context.Background(), result.ExportID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(reader.File) != len(result.Manifest.Entries)+1 {
		t.Fatalf("archive files = %d, err=%v", len(reader.File), err)
	}
}

func TestOwnerExporterExportDeleteWaitsForVerifiedPublish(t *testing.T) {
	source := &fakeOwnerExportSource{snapshot: ExportSnapshot{
		IdentitySnapshotID: "identity", ConversationSnapshotID: "conversation", ConversationJSON: []byte(`{"turns":[]}`),
	}}
	destination := NewMemoryExportDestination(1 << 20)
	deleter := &fakeOwnerDelete{}
	exporter := &OwnerExporter{Source: source, Destination: destination, Deleter: deleter, PolicyVersion: "v1"}
	if _, err := exporter.ExportDelete(context.Background(), "owner", "conversation"); err != nil {
		t.Fatal(err)
	}
	if deleter.calls != 1 {
		t.Fatalf("delete calls = %d", deleter.calls)
	}

	deleter.calls = 0
	destination.failPut = errors.New("publish failed")
	if _, err := exporter.ExportDelete(context.Background(), "owner", "conversation"); err == nil {
		t.Fatal("publish failure succeeded")
	}
	if deleter.calls != 0 {
		t.Fatalf("publish failure started %d deletes", deleter.calls)
	}
}

func TestOwnerExporterForeignOwnerReturnsNoDataAndNoDelete(t *testing.T) {
	source := &fakeOwnerExportSource{err: ErrOwnerExportNotFound}
	destination := NewMemoryExportDestination(1 << 20)
	deleter := &fakeOwnerDelete{}
	exporter := &OwnerExporter{Source: source, Destination: destination, Deleter: deleter, PolicyVersion: "v1"}
	if _, err := exporter.ExportDelete(context.Background(), "foreign", "conversation"); !errors.Is(err, ErrOwnerExportNotFound) {
		t.Fatalf("ExportDelete error = %v", err)
	}
	if deleter.calls != 0 || destination.Count() != 0 {
		t.Fatalf("foreign export writes/deletes = %d/%d", destination.Count(), deleter.calls)
	}
}

func TestOwnerExporterRejectsTraversalAndReplacedSize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		size     int64
		body     string
	}{
		{"traversal", "../escape", 1, "x"},
		{"backslash", `dir\\escape`, 1, "x"},
		{"replaced size", "safe.txt", 99, "short"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &fakeOwnerExportSource{snapshot: ExportSnapshot{
				IdentitySnapshotID: "identity", ConversationSnapshotID: "conversation",
				ConversationJSON: []byte(`{}`), Artifacts: []ExportArtifact{{ID: "asset", Filename: tc.filename, Size: tc.size}},
			}, bodies: map[string][]byte{"asset": []byte(tc.body)}}
			destination := NewMemoryExportDestination(1 << 20)
			exporter := &OwnerExporter{Source: source, Destination: destination, PolicyVersion: "v1"}
			if _, err := exporter.Export(context.Background(), "owner", "conversation"); err == nil {
				t.Fatal("unsafe export succeeded")
			}
			if destination.Count() != 0 {
				t.Fatal("unsafe export published an archive")
			}
		})
	}
}

type fakeOwnerExportSource struct {
	snapshot ExportSnapshot
	bodies   map[string][]byte
	err      error
}

func (f *fakeOwnerExportSource) Snapshot(context.Context, string, string) (ExportSnapshot, error) {
	return f.snapshot, f.err
}

func (f *fakeOwnerExportSource) OpenArtifact(_ context.Context, _, id string) (io.ReadCloser, error) {
	body, ok := f.bodies[id]
	if !ok {
		return nil, ErrOwnerExportNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type fakeOwnerDelete struct{ calls int }

func (f *fakeOwnerDelete) DeleteConversationLifecycle(context.Context, string, string) (int64, error) {
	f.calls++
	return 1, nil
}
