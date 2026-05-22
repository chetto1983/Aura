package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/xuri/excelize/v2"
)

// docTestStore is a minimal source.Writer that captures bytes per Put.
type docTestStore struct {
	puts []source.PutInput
}

func (d *docTestStore) Put(_ context.Context, in source.PutInput) (*source.Source, bool, error) {
	d.puts = append(d.puts, in)
	return &source.Source{
		ID:        "src_test_" + filepath.Base(in.Filename),
		Kind:      in.Kind,
		Filename:  in.Filename,
		MimeType:  in.MimeType,
		SizeBytes: int64(len(in.Bytes)),
		SHA256:    "deadbeef",
		Status:    source.StatusStored,
	}, false, nil
}

func (d *docTestStore) Update(id string, fn func(*source.Source) error) (*source.Source, error) {
	// Re-materialize the most-recently-Put source under id and apply fn.
	// CreateXLSXTool flips Status to Ingested via this call; returning nil
	// would clobber the live src pointer in the caller.
	for i := len(d.puts) - 1; i >= 0; i-- {
		if "src_test_"+filepath.Base(d.puts[i].Filename) == id {
			s := &source.Source{
				ID:        id,
				Kind:      d.puts[i].Kind,
				Filename:  d.puts[i].Filename,
				MimeType:  d.puts[i].MimeType,
				SizeBytes: int64(len(d.puts[i].Bytes)),
				SHA256:    "deadbeef",
				Status:    source.StatusStored,
			}
			if fn != nil {
				_ = fn(s)
			}
			return s, nil
		}
	}
	return nil, nil
}

func (d *docTestStore) Delete(_ context.Context, _ string) error { return nil }

func TestDocTool_Schema(t *testing.T) {
	tool := NewDocTool(&docTestStore{}, nil)
	if tool.Name() != "doc" {
		t.Fatalf("name = %q, want doc", tool.Name())
	}
	params := tool.Parameters()
	required, _ := params["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v, want [action]", required)
	}
	props, _ := params["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if len(enum) != 3 {
		t.Fatalf("action enum = %v, want [xlsx docx pdf]", enum)
	}
}

func TestDocTool_RegistersAsSingleDocumentSurface(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewDocTool(&docTestStore{}, nil)
	registry.Register(tool)

	if got := registry.Names(); len(got) != 1 || got[0] != "doc" {
		t.Fatalf("registry names = %#v, want [doc]", got)
	}
	for _, legacy := range []string{"create_xlsx", "create_docx", "create_pdf", "create_document"} {
		if registry.Get(legacy) != nil {
			t.Fatalf("legacy document tool %q must not be registered", legacy)
		}
	}
}

func TestDocTool_MissingAction(t *testing.T) {
	tool := NewDocTool(&docTestStore{}, nil)
	_, err := tool.Execute(t.Context(), map[string]any{})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("missing action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestDocTool_UnknownAction(t *testing.T) {
	tool := NewDocTool(&docTestStore{}, nil)
	_, err := tool.Execute(t.Context(), map[string]any{"action": "rtf"})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("unknown action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestDocTool_NilStore(t *testing.T) {
	if NewDocTool(nil, nil) != nil {
		t.Fatal("nil store should yield nil tool")
	}
}

func TestDocTool_XLSXDispatches(t *testing.T) {
	store := &docTestStore{}
	tool := NewDocTool(store, nil)
	_, err := tool.Execute(t.Context(), map[string]any{
		"action":   "xlsx",
		"filename": "dispatch.xlsx",
		"deliver":  false,
		"sheets":   []any{map[string]any{"name": "S1", "rows": []any{[]any{"a", "b"}}}},
	})
	if err != nil {
		t.Fatalf("xlsx dispatch: %v", err)
	}
	if len(store.puts) != 1 || store.puts[0].Kind != source.KindXLSX {
		t.Fatalf("expected one xlsx put, got: %+v", store.puts)
	}
	assertXLSXCell(t, store.puts[0].Bytes, "S1", "A1", "a")
}

func TestDocTool_DOCXDispatches(t *testing.T) {
	store := &docTestStore{}
	tool := NewDocTool(store, nil)
	_, err := tool.Execute(t.Context(), map[string]any{
		"action":   "docx",
		"filename": "dispatch.docx",
		"deliver":  false,
		"title":    "T",
		"blocks":   []any{map[string]any{"kind": "paragraph", "text": "Body"}},
	})
	if err != nil {
		t.Fatalf("docx dispatch: %v", err)
	}
	if len(store.puts) != 1 || store.puts[0].Kind != source.KindDOCX {
		t.Fatalf("expected one docx put, got: %+v", store.puts)
	}
	assertZipPartContains(t, store.puts[0].Bytes, "word/document.xml", "Body")
}

func TestDocTool_PDFDispatches(t *testing.T) {
	store := &docTestStore{}
	tool := NewDocTool(store, nil)
	_, err := tool.Execute(t.Context(), map[string]any{
		"action":   "pdf",
		"filename": "dispatch.pdf",
		"deliver":  false,
		"title":    "T",
		"blocks":   []any{map[string]any{"kind": "paragraph", "text": "Body"}},
	})
	if err != nil {
		t.Fatalf("pdf dispatch: %v", err)
	}
	if len(store.puts) != 1 || store.puts[0].Kind != source.KindPDFGen {
		t.Fatalf("expected one pdf put, got: %+v", store.puts)
	}
	if !bytes.HasPrefix(store.puts[0].Bytes, []byte("%PDF-")) {
		t.Fatalf("pdf bytes missing %%PDF header: %q", store.puts[0].Bytes[:min(len(store.puts[0].Bytes), 8)])
	}
}

func assertXLSXCell(t *testing.T, body []byte, sheet, cell, want string) {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("xlsx OpenReader: %v", err)
	}
	defer f.Close()
	got, err := f.GetCellValue(sheet, cell)
	if err != nil {
		t.Fatalf("xlsx GetCellValue: %v", err)
	}
	if got != want {
		t.Fatalf("xlsx %s!%s = %q, want %q", sheet, cell, got, want)
	}
}

func assertZipPartContains(t *testing.T, body []byte, part, want string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != part {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", part, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", part, err)
		}
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s missing %q:\n%s", part, want, string(b))
		}
		return
	}
	t.Fatalf("zip part %q not found", part)
}
