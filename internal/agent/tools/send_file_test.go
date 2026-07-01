package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSendFileSpecIsDeferred pins the deferred-tool contract: send_file has a
// path/caption schema + an inline example, so it MUST be Deferred (full spec
// loaded on demand via tool_search) and Mutating:false (it reads a file and
// emits an artifact descriptor — no host mutation). RESEARCH §5 / OQ3.
func TestSendFileSpecIsDeferred(t *testing.T) {
	s := (&SendFile{}).Spec()
	if s.Name != "send_file" {
		t.Fatalf("name = %q, want send_file", s.Name)
	}
	if !s.Deferred {
		t.Fatal("send_file MUST be Deferred:true — it has a path/caption schema + example (deferred-tool rule)")
	}
	if s.Mutating {
		t.Fatal("send_file MUST be Mutating:false — it reads a file and emits a descriptor, no host mutation")
	}
	if s.Summary == "" {
		t.Fatal("send_file Spec must carry a one-line Summary (manifest-visible)")
	}
	// The deferred full Description carries the inline example the model loads on
	// demand; assert the path/caption schema is declared.
	for _, needle := range []string{"path", "caption"} {
		if !strings.Contains(string(s.Parameters), needle) {
			t.Fatalf("send_file Parameters missing %q field: %s", needle, s.Parameters)
		}
	}
}

// TestSendFileChannelAgnostic pins D-06: the substrate never names Telegram. The
// artifact path must stay channel-agnostic — each channel renders the event its
// own way downstream (this is the acceptance grep guard).
func TestSendFileChannelAgnostic(t *testing.T) {
	data, err := os.ReadFile("send_file.go")
	if err != nil {
		t.Fatalf("read send_file.go: %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "telegram") {
		t.Fatal("send_file.go must NOT mention telegram — the substrate stays channel-agnostic (D-06)")
	}
}

// TestSendFileExecuteSetsArtifactMeta is the happy path: a small readable file
// returns a ToolResult whose Meta carries an `artifact` descriptor
// {path, filename, caption}. The tool does NOT touch the Event (it cannot — the
// inbound ctx is read-only); the agent loop lifts the Meta onto ArtifactDelta.
func TestSendFileExecuteSetsArtifactMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.xlsx")
	if err := os.WriteFile(path, []byte("small spreadsheet bytes"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ctx := ctxWith(t, "sess-sf", "call-sf")

	raw, _ := json.Marshal(map[string]string{"path": path, "caption": "results"})
	res, err := (&SendFile{}).Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta == nil {
		t.Fatal("Meta is nil, want an artifact descriptor")
	}
	art, ok := (*res.Meta)["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("Meta[artifact] = %#v, want map[string]any", (*res.Meta)["artifact"])
	}
	if got := art["path"]; got != path {
		t.Fatalf("artifact.path = %v, want %q", got, path)
	}
	if got := art["filename"]; got != "results.xlsx" {
		t.Fatalf("artifact.filename = %v, want results.xlsx (filepath.Base)", got)
	}
	if got := art["caption"]; got != "results" {
		t.Fatalf("artifact.caption = %v, want results", got)
	}
	if res.Preview == "" {
		t.Fatal("Preview must be a short confirmation the model can narrate")
	}
	if res.Bytes != len(res.Preview) {
		t.Fatalf("Bytes = %d, want preview length %d", res.Bytes, len(res.Preview))
	}
}

func TestSendFileAllowsWorkspacePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path})
	res, err := (&SendFile{WorkspaceRoot: root}).Execute(ctxWith(t, "sess-sf-root", "call-sf-root"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta == nil {
		t.Fatal("workspace file must produce artifact meta")
	}
	art := (*res.Meta)["artifact"].(map[string]any)
	// SendFile returns the EvalSymlinks-resolved workspace path (the fence at
	// send_file.go:136). Compare against the resolved form, not the raw join, so the
	// test is robust to a t.TempDir() carrying a symlink (macOS /var) or an 8.3 short
	// name (a Windows CI runner whose user > 8 chars yields RUNNER~1, while the fence
	// resolves it to the long form).
	want, serr := filepath.EvalSymlinks(path)
	if serr != nil {
		t.Fatalf("evalsymlinks want: %v", serr)
	}
	if got := art["path"]; got != want {
		t.Fatalf("artifact.path = %v, want resolved path %q", got, want)
	}
}

func TestSendFileAllowsWorkspacePathWithDotDotPrefixName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "..report.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path})
	res, err := (&SendFile{WorkspaceRoot: root}).Execute(ctxWith(t, "sess-sf-dotdot-name", "call-sf-dotdot-name"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta == nil {
		t.Fatalf("in-workspace filename beginning with '..' must produce artifact meta, got %q", res.Preview)
	}
}

func TestSendFileRejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"path": outside})
	res, err := (&SendFile{WorkspaceRoot: root}).Execute(ctxWith(t, "sess-sf-out", "call-sf-out"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("outside-workspace path must not produce artifact meta")
	}
	if !strings.Contains(res.Preview, "outside_workspace_unsupported") {
		t.Fatalf("want deterministic outside_workspace_unsupported result, got %q", res.Preview)
	}
	// The dead approval route (F-009) must be gone: no ask_user + resume_context — that
	// pair advertised an unwireable loop no resume hook consumed. The reject must
	// instead point the model at the one working fix: copy into the workspace.
	for _, banned := range []string{"ask_user", "resume_context"} {
		if strings.Contains(res.Preview, banned) {
			t.Fatalf("outside-workspace reject must not advertise %q (dead route), got %q", banned, res.Preview)
		}
	}
	if !strings.Contains(res.Preview, "copy") || !strings.Contains(res.Preview, "workspace") {
		t.Fatalf("reject must instruct copying the file into the workspace, got %q", res.Preview)
	}
}

func TestSendFileRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires privileges on some runners")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "linked-secret.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"path": link})
	res, err := (&SendFile{WorkspaceRoot: root}).Execute(ctxWith(t, "sess-sf-link", "call-sf-link"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta != nil || !strings.Contains(res.Preview, "outside_workspace_unsupported") {
		t.Fatalf("symlink escape must be refused, got meta=%v preview=%q", res.Meta, res.Preview)
	}
}

func TestSendFileNoWorkspacePreservesLegacyBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.txt")
	if err := os.WriteFile(path, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path})
	res, err := (&SendFile{}).Execute(ctxWith(t, "sess-sf-legacy", "call-sf-legacy"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta == nil {
		t.Fatal("empty WorkspaceRoot must preserve legacy artifact delivery")
	}
}

// TestSendFileCaptionASCIISanitized pins Pitfall 4 / T-13-02-CaptionInject: a
// non-ASCII caption is sanitized so a downstream document/voice send never 400s
// on a non-ASCII caption. The descriptor's caption carries only ASCII bytes.
func TestSendFileCaptionASCIISanitized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, []byte("pdf bytes"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ctx := ctxWith(t, "sess-sf-ascii", "call-sf")

	raw, _ := json.Marshal(map[string]string{"path": path, "caption": "città è caffè — résumé"})
	res, err := (&SendFile{}).Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	art := (*res.Meta)["artifact"].(map[string]any)
	caption, _ := art["caption"].(string)
	for i := 0; i < len(caption); i++ {
		if caption[i] > 127 {
			t.Fatalf("caption %q carries a non-ASCII byte at %d — must be sanitized (Pitfall 4)", caption, i)
		}
	}
	if caption == "" {
		t.Fatalf("sanitized caption collapsed to empty for %q; want a best-effort ASCII rendering", "città è caffè — résumé")
	}
}

// TestSendFileTooLargeErrors pins T-13-02-Artifact / OQ3: a >50MB file returns an
// error ToolResult {error:"file_too_large"} the agent surfaces — NEVER a silent
// truncation, and NO artifact Meta on overflow.
func TestSendFileTooLargeErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	// Sparse file just over the 50MB cap — Truncate sets the size without writing.
	if err := f.Truncate(maxSendFileBytes + 1); err != nil {
		f.Close()
		t.Fatalf("truncate: %v", err)
	}
	f.Close()
	ctx := ctxWith(t, "sess-sf-big", "call-sf")

	raw, _ := json.Marshal(map[string]string{"path": path})
	res, err := (&SendFile{}).Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute must surface the overflow as a result, not a Go error: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("an oversized file must NOT carry an artifact descriptor (no silent send)")
	}
	if !strings.Contains(res.Preview, "file_too_large") {
		t.Fatalf("oversized result must carry file_too_large, got: %q", res.Preview)
	}
}

// TestSendFileUnreadableErrors: a nonexistent/unreadable path returns an error
// ToolResult {error:"file_unreadable"} the model self-corrects on, with no Meta.
func TestSendFileUnreadableErrors(t *testing.T) {
	ctx := ctxWith(t, "sess-sf-bad", "call-sf")
	raw, _ := json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "does-not-exist.txt")})
	res, err := (&SendFile{}).Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute must surface an unreadable path as a result, not a Go error: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("an unreadable path must NOT carry an artifact descriptor")
	}
	if !strings.Contains(res.Preview, "file_unreadable") {
		t.Fatalf("unreadable result must carry file_unreadable, got: %q", res.Preview)
	}
}

// TestSendFileRejectsDirectory: a directory path is not a deliverable file — it
// returns file_unreadable (a stat succeeds but IsDir), never an artifact.
func TestSendFileRejectsDirectory(t *testing.T) {
	ctx := ctxWith(t, "sess-sf-dir", "call-sf")
	raw, _ := json.Marshal(map[string]string{"path": t.TempDir()})
	res, err := (&SendFile{}).Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("a directory must NOT carry an artifact descriptor")
	}
	if !strings.Contains(res.Preview, "file_unreadable") {
		t.Fatalf("directory result must carry file_unreadable, got: %q", res.Preview)
	}
}

// TestSendFileMissingPath: an empty path arg is a self-correctable error result.
func TestSendFileMissingPath(t *testing.T) {
	ctx := ctxWith(t, "sess-sf-empty", "call-sf")
	res, err := (&SendFile{}).Execute(ctx, json.RawMessage(`{"caption":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("a missing path must NOT carry an artifact descriptor")
	}
	if !strings.Contains(res.Preview, "file_unreadable") {
		t.Fatalf("missing-path result must carry file_unreadable, got: %q", res.Preview)
	}
}
