package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorePutCreatesSource(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	src, dup, err := s.Put(context.Background(), PutInput{
		Kind:     KindPDF,
		Filename: "paper.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF-1.4 fake"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if dup {
		t.Errorf("expected new source, got duplicate")
	}
	if !strings.HasPrefix(src.ID, idPrefix) {
		t.Errorf("ID = %q, want %s prefix", src.ID, idPrefix)
	}
	if src.Status != StatusStored {
		t.Errorf("Status = %q, want %q", src.Status, StatusStored)
	}
	if src.SizeBytes != int64(len("%PDF-1.4 fake")) {
		t.Errorf("SizeBytes = %d", src.SizeBytes)
	}
	if src.CreatedAt.IsZero() {
		t.Errorf("CreatedAt is zero")
	}

	if _, err := os.Stat(filepath.Join(dir, "raw", src.ID, "original.pdf")); err != nil {
		t.Errorf("original.pdf missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "raw", src.ID, "source.json")); err != nil {
		t.Errorf("source.json missing: %v", err)
	}
}

func TestStorePutDedupBySha256(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)

	in := PutInput{Kind: KindPDF, Filename: "a.pdf", MimeType: "application/pdf", Bytes: []byte("identical content")}
	src1, dup1, err := s.Put(context.Background(), in)
	if err != nil || dup1 {
		t.Fatalf("first put: dup=%v err=%v", dup1, err)
	}

	in.Filename = "totally-different-name.pdf"
	src2, dup2, err := s.Put(context.Background(), in)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
	if !dup2 {
		t.Errorf("expected dup=true on identical content")
	}
	if src1.ID != src2.ID {
		t.Errorf("dedup IDs differ: %s vs %s", src1.ID, src2.ID)
	}
	if src2.Filename != "a.pdf" {
		t.Errorf("dedup should return original filename, got %q", src2.Filename)
	}
}

func TestStoreGetReturnsNotExist(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	_, err := s.Get("src_0000000000000000")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestStoreGetRejectsInvalidID(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	cases := []string{
		"",
		"foo",
		"src_xx",
		"src_0000000000000000extra",
		"src_GGGGGGGGGGGGGGGG",
		"../escape",
		"src_0000000000000000/escape",
	}
	for _, id := range cases {
		if _, err := s.Get(id); err == nil {
			t.Errorf("Get(%q) expected error, got nil", id)
		}
	}
}

func TestStoreListFilters(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	pdfSrc, _, _ := s.Put(context.Background(), PutInput{Kind: KindPDF, Filename: "a.pdf", Bytes: []byte("a")})
	txtSrc, _, _ := s.Put(context.Background(), PutInput{Kind: KindText, Filename: "b.txt", Bytes: []byte("b")})

	if _, err := s.Update(txtSrc.ID, func(rec *Source) error {
		rec.Status = StatusIngested
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	all, err := s.List(ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List all: got %d, want 2", len(all))
	}

	pdfs, _ := s.List(ListFilter{Kind: KindPDF})
	if len(pdfs) != 1 || pdfs[0].ID != pdfSrc.ID {
		t.Errorf("Kind filter: %+v", pdfs)
	}

	ingested, _ := s.List(ListFilter{Status: StatusIngested})
	if len(ingested) != 1 || ingested[0].ID != txtSrc.ID {
		t.Errorf("Status filter: %+v", ingested)
	}

	none, _ := s.List(ListFilter{Status: StatusFailed})
	if len(none) != 0 {
		t.Errorf("Empty filter: %+v", none)
	}
}

func TestStoreListSkipsBogusEntries(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	if _, _, err := s.Put(context.Background(), PutInput{Kind: KindPDF, Filename: "a.pdf", Bytes: []byte("a")}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Inject bogus dirs that List must ignore.
	_ = os.MkdirAll(filepath.Join(s.RawDir(), "not-a-source-id"), 0o755)
	_ = os.MkdirAll(filepath.Join(s.RawDir(), "src_zzzzzzzzzzzzzzzz"), 0o755)
	_ = os.WriteFile(filepath.Join(s.RawDir(), "loose-file.txt"), []byte("x"), 0o644)

	got, err := s.List(ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("List: got %d entries, want 1", len(got))
	}
}

func TestStoreUpdatePersists(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	src, _, _ := s.Put(context.Background(), PutInput{Kind: KindPDF, Filename: "a.pdf", Bytes: []byte("abc")})

	updated, err := s.Update(src.ID, func(rec *Source) error {
		rec.Status = StatusOCRComplete
		rec.PageCount = 5
		rec.OCRModel = "mistral-ocr-latest"
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != StatusOCRComplete || updated.PageCount != 5 {
		t.Errorf("update fields: %+v", updated)
	}

	reread, err := s.Get(src.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if reread.PageCount != 5 || reread.OCRModel != "mistral-ocr-latest" {
		t.Errorf("update not persisted: %+v", reread)
	}
}

func TestStoreUpdateMutatorError(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	src, _, _ := s.Put(context.Background(), PutInput{Kind: KindPDF, Filename: "a.pdf", Bytes: []byte("abc")})

	wantErr := errors.New("boom")
	if _, err := s.Update(src.ID, func(rec *Source) error { return wantErr }); !errors.Is(err, wantErr) {
		t.Errorf("Update propagated error: %v", err)
	}
}

func TestStorePutValidates(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	tests := []PutInput{
		{Kind: "", Filename: "a.pdf", Bytes: []byte("x")},
		{Kind: "invalid", Filename: "a.pdf", Bytes: []byte("x")},
		{Kind: KindPDF, Filename: "", Bytes: []byte("x")},
		{Kind: KindPDF, Filename: "a.pdf", Bytes: nil},
	}
	for i, tc := range tests {
		if _, _, err := s.Put(context.Background(), tc); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestStorePathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	bad := []struct{ id, name string }{
		{"src_0000000000000000", "../escape"},
		{"src_0000000000000000", "sub/dir"},
		{"src_0000000000000000", `back\slash`},
		{"src_0000000000000000", ""},
		{"src_0000000000000000", "."},
		{"src_0000000000000000", ".."},
		{"invalid", "ocr.md"},
	}
	for _, tc := range bad {
		if got := s.Path(tc.id, tc.name); got != "" {
			t.Errorf("Path(%q,%q) = %q, want empty", tc.id, tc.name, got)
		}
	}

	good := s.Path("src_0000000000000000", "ocr.md")
	wantSuffix := filepath.Join("src_0000000000000000", "ocr.md")
	if !strings.HasSuffix(good, wantSuffix) {
		t.Errorf("Path valid case = %q, want suffix %q", good, wantSuffix)
	}
}

func TestStorePutKinds(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	cases := []struct {
		in       PutInput
		wantFile string
	}{
		{PutInput{Kind: KindPDF, Filename: "a.pdf", Bytes: []byte("a")}, "original.pdf"},
		{PutInput{Kind: KindText, Filename: "b.txt", Bytes: []byte("b")}, "original.txt"},
		{PutInput{Kind: KindURL, Filename: "c.url", Bytes: []byte("https://example.com")}, "original.url"},
	}
	for _, tc := range cases {
		src, _, err := s.Put(context.Background(), tc.in)
		if err != nil {
			t.Fatalf("Put %s: %v", tc.in.Kind, err)
		}
		if _, err := os.Stat(filepath.Join(s.RawDir(), src.ID, tc.wantFile)); err != nil {
			t.Errorf("%s: %s missing", tc.in.Kind, tc.wantFile)
		}
	}
}
