package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// openTestService returns a fake serving body under a temporary workspace root.
func openTestService(t *testing.T, body string, meta documents.OpenedDocument) *fakeDocsService {
	t.Helper()
	return &fakeDocsService{
		root:     t.TempDir(),
		openBody: io.NopCloser(strings.NewReader(body)),
		openMeta: meta,
	}
}

func runDocsOpen(t *testing.T, svc *fakeDocsService, args ...string) (map[string]any, error) {
	t.Helper()
	var out bytes.Buffer
	ctx := identityctx.WithIdentityID(context.Background(), "operator-1")
	err := runDocsCommand(ctx, append([]string{"open"}, args...), &out, fakeDocsFactory(svc))
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if jsonErr := json.Unmarshal(out.Bytes(), &decoded); jsonErr != nil {
		t.Fatalf("decode result: %v (%s)", jsonErr, out.String())
	}
	return decoded, nil
}

// TestDocsOpenWritesFileAndMeasuresDigest is the whole contract of the host arm: the
// bytes land somewhere a host caller can actually read, and the reported digest is
// measured off what was written rather than copied from the index's claim.
func TestDocsOpenWritesFileAndMeasuresDigest(t *testing.T) {
	body := "Data,Temp\n2026-09-12,20.9\n"
	svc := openTestService(t, body, documents.OpenedDocument{
		DocumentID: "doc_abc", FileName: "meteo.csv", SourceKey: "chat/x.csv",
		SizeBytes: int64(len(body)), SHA256: "claimed-by-the-index",
	})
	result, err := runDocsOpen(t, svc, "--", "doc_abc")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := result["path"].(string)
	if !strings.HasPrefix(path, svc.root) {
		t.Fatalf("path %q escaped the workspace root %q", path, svc.root)
	}
	relative, relErr := filepath.Rel(svc.root, path)
	if relErr != nil {
		t.Fatal(relErr)
	}
	relative = filepath.ToSlash(relative)
	want, wantErr := documents.StagedRelativePath("doc_abc", "meteo.csv")
	if wantErr != nil {
		t.Fatal(wantErr)
	}
	if relative != want {
		t.Fatalf("layout = %q, want the shared staged layout %q", relative, want)
	}
	written, readErr := os.ReadFile(path) // #nosec G304 -- path is the one the command just wrote
	if readErr != nil {
		t.Fatalf("the reported path is not readable: %v", readErr)
	}
	if string(written) != body {
		t.Fatalf("wrote %q, want %q", written, body)
	}
	sum := sha256.Sum256([]byte(body))
	if result["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %v, want the digest of the written bytes", result["sha256"])
	}
	if result["indexed_sha256"] != "claimed-by-the-index" {
		t.Fatalf("indexed_sha256 = %v, want the index's own claim reported beside it", result["indexed_sha256"])
	}
	if svc.openedIdentity != "operator-1" {
		t.Fatalf("opened as %q, want the context identity", svc.openedIdentity)
	}
}

// TestDocsOpenRefusesShortStream: a stream that stops early must leave NOTHING behind.
// A truncated spreadsheet that looks whole is worse than no file, because the answer
// computed from it is confident and wrong.
func TestDocsOpenRefusesShortStream(t *testing.T) {
	svc := openTestService(t, "only-part", documents.OpenedDocument{
		DocumentID: "doc_short", FileName: "clienti.xlsx", SizeBytes: 5889,
	})
	if _, err := runDocsOpen(t, svc, "--", "doc_short"); err == nil {
		t.Fatal("a short stream was accepted as a whole document")
	}
	var leftovers []string
	walkErr := filepath.WalkDir(svc.root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			leftovers = append(leftovers, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("failed open left files behind: %v", leftovers)
	}
}

// TestDocsOpenRefusesTraversalName covers the name arriving from the INDEX, which is the
// half a caller cannot see: the record's own file name becomes a path component too.
func TestDocsOpenRefusesTraversalName(t *testing.T) {
	for _, name := range []string{"../escape.txt", "sub/dir.txt", ".hidden"} {
		svc := openTestService(t, "x", documents.OpenedDocument{
			DocumentID: "doc_evil", FileName: name, SizeBytes: 1,
		})
		if _, err := runDocsOpen(t, svc, "--", "doc_evil"); err == nil {
			t.Fatalf("index file name %q was accepted as a path component", name)
		}
	}
	svc := openTestService(t, "x", documents.OpenedDocument{DocumentID: "doc_ok", FileName: "ok.txt", SizeBytes: 1})
	if _, err := runDocsOpen(t, svc, "--file-name", "../escape.txt", "--", "doc_ok"); err == nil {
		t.Fatal("caller-supplied --file-name traversal was accepted")
	}
}

func TestDocsOpenRequiresDocumentID(t *testing.T) {
	svc := openTestService(t, "x", documents.OpenedDocument{})
	if _, err := runDocsOpen(t, svc); err == nil {
		t.Fatal("docs open accepted a missing document id")
	}
}

func TestDocsOpenPropagatesBackendFailure(t *testing.T) {
	svc := &fakeDocsService{root: t.TempDir(), openErr: errors.New("object store is down")}
	if _, err := runDocsOpen(t, svc, "--", "doc_1"); err == nil ||
		!strings.Contains(err.Error(), "object store is down") {
		t.Fatalf("err = %v, want the backend failure surfaced", err)
	}
}

// TestDocsMCPDocumentOpen proves the MCP call lands in the AGENT's document_open, with the
// caller's arguments plumbed through -- it used to prove the CLI verb ran instead, which is
// why it asserted a host path this tool never writes.
func TestDocsMCPDocumentOpen(t *testing.T) {
	body := "hello from garage"
	svc := openTestService(t, body, documents.OpenedDocument{
		DocumentID: "doc_mcp", FileName: "original.txt", SizeBytes: int64(len(body)),
	})
	session := docsMCPSession(t, svc)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "document_open", Arguments: map[string]any{"document_id": "doc_mcp", "file_name": "alias.txt"},
	})
	if err != nil || result.IsError {
		t.Fatalf("call: %v, %#v", err, result)
	}
	payload, marshalErr := json.Marshal(result.StructuredContent)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if !bytes.Contains(payload, []byte(`"document_open"`)) {
		t.Fatalf("answer did not come from the agent tool: %s", payload)
	}
	// The bytes landing where the caller can read them is the tool's own subject and is
	// measured in its package, against a real box; here the question is only whose code ran.
	escaped, escapeErr := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "document_open", Arguments: map[string]any{"document_id": "doc_mcp", "file_name": "../escape.txt"},
	})
	if escapeErr == nil && !escaped.IsError {
		t.Fatal("the agent tool's own file-name rule did not run")
	}
}
