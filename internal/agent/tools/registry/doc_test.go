package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/sources/store"
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

type docTestSender struct {
	calls int
}

func (d *docTestSender) SendDocumentToUser(_, _ string, _ []byte, _ string) error {
	d.calls++
	return nil
}

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
}
