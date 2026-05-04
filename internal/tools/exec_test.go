package tools_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
)

func TestExecuteCodeTool_NilManager(t *testing.T) {
	tool := tools.NewExecuteCodeTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when manager is nil")
	}
}

func TestExecuteCodeTool_DescriptionDefersSimpleDocumentsToTypedTools(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{OK: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := tools.NewExecuteCodeTool(manager)
	desc := tool.Description()
	for _, want := range []string{"Use create_xlsx/create_docx/create_pdf", "for simple documents", "/tmp/aura_out", "computed artifacts"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
	}
}

func TestExecuteCodeTool_DeliversArtifacts(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{
			OK:        true,
			Stdout:    "created plot\n",
			ExitCode:  0,
			ElapsedMs: 11,
			Artifacts: []sandbox.Artifact{{
				Name:      "plot.png",
				MimeType:  "image/png",
				Bytes:     []byte("png-bytes"),
				SizeBytes: 9,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := &execArtifactSender{}
	tool := tools.NewExecuteCodeToolWithSender(manager, sender)
	if tool == nil {
		t.Fatal("tool = nil")
	}

	out, err := tool.Execute(tools.WithUserID(context.Background(), "12345"), map[string]any{
		"code": "make plot",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(sender.sent))
	}
	got := sender.sent[0]
	if got.userID != "12345" || got.filename != "plot.png" || string(got.body) != "png-bytes" {
		t.Fatalf("sent = %+v", got)
	}
	if got.caption == "" {
		t.Fatal("caption is empty")
	}
	if !containsAll(out, "artifacts:", "plot.png", "delivered=true") {
		t.Fatalf("output = %q", out)
	}
}

func TestExecuteCodeTool_PersistsArtifactsAsSources(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{
			OK:        true,
			Stdout:    "created csv\n",
			ExitCode:  0,
			ElapsedMs: 12,
			Artifacts: []sandbox.Artifact{{
				Name:      "metrics.csv",
				MimeType:  "text/csv",
				Bytes:     []byte("name,value\naura,1\n"),
				SizeBytes: int64(len("name,value\naura,1\n")),
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sender := &execArtifactSender{}
	tool := tools.NewExecuteCodeToolWithStore(manager, sender, store)
	if tool == nil {
		t.Fatal("tool = nil")
	}

	out, err := tool.Execute(tools.WithUserID(context.Background(), "12345"), map[string]any{
		"code": "make csv",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !containsAll(out, "artifacts:", "metrics.csv", "delivered=true", "persisted=true", "source_id=src_") {
		t.Fatalf("output = %q", out)
	}

	rows, err := store.List(source.ListFilter{Kind: source.KindSandboxArtifact})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(rows))
	}
	rec := rows[0]
	if rec.Filename != "metrics.csv" || rec.MimeType != "text/csv" || rec.Status != source.StatusIngested {
		t.Fatalf("stored source = %+v", rec)
	}
	if _, err := os.Stat(store.Path(rec.ID, "original.csv")); err != nil {
		t.Fatal(err)
	}
}

type fakeExecRuntime struct {
	result *sandbox.Result
	err    error
}

func (r fakeExecRuntime) Kind() sandbox.RuntimeKind { return sandbox.RuntimeKindPyodide }

func (r fakeExecRuntime) Execute(context.Context, string, bool) (*sandbox.Result, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}

func (fakeExecRuntime) CheckAvailability() sandbox.Availability {
	return sandbox.Availability{Available: true, Kind: sandbox.RuntimeKindPyodide, Detail: "ok"}
}

func (fakeExecRuntime) ValidateCode(string) error { return nil }

type execArtifactSender struct {
	sent []execArtifactSend
}

type execArtifactSend struct {
	userID   string
	filename string
	body     []byte
	caption  string
}

func (s *execArtifactSender) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	s.sent = append(s.sent, execArtifactSend{userID: userID, filename: filename, body: body, caption: caption})
	return nil
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
