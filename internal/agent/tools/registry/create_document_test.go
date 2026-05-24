package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	source "github.com/aura/aura/internal/storage/sources/store"
)

func newCreateDocumentTest(t *testing.T, sender DocumentSender) (*CreateDocumentTool, *source.Store) {
	t.Helper()
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	tool := NewCreateDocumentTool(store, sender)
	if tool == nil {
		t.Fatal("NewCreateDocumentTool returned nil")
	}
	return tool, store
}

func TestCreateDocumentTool_DispatchesAllFormats(t *testing.T) {
	cases := []struct {
		name     string
		format   string
		spec     map[string]any
		filename string
		kind     source.Kind
	}{
		{
			name:   "pdf",
			format: "pdf",
			spec: map[string]any{
				"filename": "report",
				"title":    "Quarterly Report",
				"blocks": []any{
					map[string]any{"kind": "paragraph", "text": "Revenue improved."},
				},
				"deliver": false,
			},
			filename: "report.pdf",
			kind:     source.KindPDFGen,
		},
		{
			name:   "xlsx",
			format: "xlsx",
			spec: map[string]any{
				"filename": "report",
				"sheets": []any{
					map[string]any{
						"name": "Q1",
						"rows": []any{
							[]any{"Name", "Age"},
							[]any{"Ada", 37.0},
						},
					},
				},
				"deliver": false,
			},
			filename: "report.xlsx",
			kind:     source.KindXLSX,
		},
		{
			name:   "docx",
			format: "docx",
			spec: map[string]any{
				"filename": "memo",
				"title":    "Project Memo",
				"blocks": []any{
					map[string]any{"kind": "paragraph", "text": "Keep it concise."},
				},
				"deliver": false,
			},
			filename: "memo.docx",
			kind:     source.KindDOCX,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, store := newCreateDocumentTest(t, nil)
			out, err := tool.Execute(context.Background(), map[string]any{
				"format": tc.format,
				"spec":   tc.spec,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			var resp map[string]any
			if err := json.Unmarshal([]byte(out), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if resp["filename"] != tc.filename {
				t.Fatalf("filename = %v, want %s", resp["filename"], tc.filename)
			}
			if resp["delivered"] != false {
				t.Fatalf("delivered = %v, want false", resp["delivered"])
			}
			if resp["follow_up_required"] != false {
				t.Fatalf("follow_up_required = %v, want false", resp["follow_up_required"])
			}
			rec, err := store.Get(resp["source_id"].(string))
			if err != nil {
				t.Fatalf("store.Get: %v", err)
			}
			if rec.Kind != tc.kind {
				t.Fatalf("kind = %q, want %q", rec.Kind, tc.kind)
			}
			if rec.Status != source.StatusIngested {
				t.Fatalf("status = %q, want ingested", rec.Status)
			}
		})
	}
}

func TestCreateDocumentTool_RejectsUnknownFormat(t *testing.T) {
	tool, _ := newCreateDocumentTest(t, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"format": "pptx",
		"spec":   map[string]any{"filename": "slides"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("error = %v, want unsupported format", err)
	}
}

func TestCreateDocumentTool_RejectsMissingSpec(t *testing.T) {
	tool, _ := newCreateDocumentTest(t, nil)
	_, err := tool.Execute(context.Background(), map[string]any{"format": "pdf"})
	if err == nil || !strings.Contains(err.Error(), "spec object is required") {
		t.Fatalf("error = %v, want missing spec", err)
	}
}

func TestCreateDocumentTool_DuplicateResponseUsesStoredFilename(t *testing.T) {
	tool, _ := newCreateDocumentTest(t, nil)
	spec := func(filename string) map[string]any {
		return map[string]any{
			"filename": filename,
			"sheets": []any{
				map[string]any{
					"name": "Data",
					"rows": []any{
						[]any{"Name", "Age"},
						[]any{"Ada", 37.0},
					},
				},
			},
			"deliver": false,
		}
	}

	firstOut, err := tool.Execute(context.Background(), map[string]any{
		"format": "xlsx",
		"spec":   spec("first-name"),
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	secondOut, err := tool.Execute(context.Background(), map[string]any{
		"format": "xlsx",
		"spec":   spec("second-name"),
	})
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	var first, second map[string]any
	if err := json.Unmarshal([]byte(firstOut), &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if err := json.Unmarshal([]byte(secondOut), &second); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	if first["source_id"] != second["source_id"] {
		t.Fatalf("source_id changed: first=%v second=%v", first["source_id"], second["source_id"])
	}
	if second["duplicate"] != true {
		t.Fatalf("duplicate = %v, want true", second["duplicate"])
	}
	if second["filename"] != first["filename"] {
		t.Fatalf("duplicate filename = %v, want stored filename %v", second["filename"], first["filename"])
	}
}

func TestCreateDocumentTool_NilStoreReturnsNil(t *testing.T) {
	if tool := NewCreateDocumentTool(nil, nil); tool != nil {
		t.Fatal("NewCreateDocumentTool(nil, nil) should return nil")
	}
}

func TestCreateDocumentTool_NilSenderFailsOnlyWhenDelivering(t *testing.T) {
	tool, _ := newCreateDocumentTest(t, nil)
	ctx := WithUserID(context.Background(), "12345")
	spec := map[string]any{
		"filename": "memo",
		"title":    "Memo",
	}

	_, err := tool.Execute(ctx, map[string]any{"format": "docx", "spec": spec})
	if err == nil || !strings.Contains(err.Error(), "DocumentSender configured") {
		t.Fatalf("error = %v, want missing sender", err)
	}

	spec["deliver"] = false
	if _, err := tool.Execute(ctx, map[string]any{"format": "docx", "spec": spec}); err != nil {
		t.Fatalf("deliver=false with nil sender: %v", err)
	}
}

func TestCreateDocumentTool_DescriptionStaysShort(t *testing.T) {
	tool, _ := newCreateDocumentTest(t, nil)
	if n := len(tool.Description()); n > 200 {
		t.Fatalf("description length = %d, want <= 200", n)
	}
}
