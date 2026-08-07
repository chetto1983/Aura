package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
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
	// 2026-08-07: promoted to always-active when the set was aligned to the reference coding
	// agent's own tools, whose delivery tool is always in the manifest. Delivering a result is
	// the last step of most requests, so paying a tool_search for it taxed the common path.
	// See TestOnlyTheWorkingSetIsAlwaysActive for the full rationale.
	if s.Deferred {
		t.Fatal("send_file is part of the always-active working set — see always_active_test.go")
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
	ctx := ctxWith(t, "sess-sf", "call-sf")

	raw, _ := json.Marshal(map[string]string{"path": "/workspace/results.xlsx", "caption": "results"})
	res, err := (&SendFile{Router: boxDelivery(t, "results.xlsx", "small spreadsheet bytes")}).Execute(ctx, raw)
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
	// The descriptor path is the STAGED host-side copy, not the box path: the delivery pipeline
	// (telegram tele.FromDisk, asset ingest) reads it from the host after the turn.
	staged, _ := art["path"].(string)
	if staged == "" || staged == "/workspace/results.xlsx" {
		t.Fatalf("artifact.path = %v, want the staged host copy", art["path"])
	}
	if body, rerr := os.ReadFile(staged); rerr != nil || string(body) != "small spreadsheet bytes" {
		t.Fatalf("staged copy = %q err %v, want the box artifact's bytes", body, rerr)
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

// The fence is now a literal /workspace prefix check on the BOX path, applied BEFORE the artifact
// is copied out. It replaces the host EvalSymlinks fence (and its symlink-escape and
// dot-dot-prefix cases): a box path cannot be host-stat'd, so the escape it has to refuse is a
// path naming something outside the box's workspace mount. The symlink-resolving fence itself is
// not gone — fenceWithinRoot still runs over the STAGED copy (send_file_sandbox.go).
func TestSendFileFencesToBoxWorkspace(t *testing.T) {
	for _, outside := range []string{"/etc/passwd", "/skills/calc/calc.py", "/workspace/../etc/shadow"} {
		t.Run(outside, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"path": outside})
			res, err := (&SendFile{Router: boxDelivery(t, "leak.txt", "leaked")}).
				Execute(ctxWith(t, "sess-sf-out", "call-sf-out"), raw)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Meta != nil {
				t.Fatal("an out-of-workspace path must not produce artifact meta")
			}
			if !strings.Contains(res.Preview, "outside_workspace_unsupported") {
				t.Fatalf("want deterministic outside_workspace_unsupported result, got %q", res.Preview)
			}
			// The dead approval route (F-009) must stay gone: no ask_user + resume_context — that
			// pair advertised an unwireable loop no resume hook consumed. The reject instead points
			// the model at the one working fix: copy into the workspace.
			for _, banned := range []string{"ask_user", "resume_context"} {
				if strings.Contains(res.Preview, banned) {
					t.Fatalf("outside-workspace reject must not advertise %q (dead route), got %q", banned, res.Preview)
				}
			}
			if !strings.Contains(res.Preview, "copy") || !strings.Contains(res.Preview, "workspace") {
				t.Fatalf("reject must instruct copying the file into the workspace, got %q", res.Preview)
			}
		})
	}
}

// TestSendFileCaptionASCIISanitized pins Pitfall 4 / T-13-02-CaptionInject: a
// non-ASCII caption is sanitized so a downstream document/voice send never 400s
// on a non-ASCII caption. The descriptor's caption carries only ASCII bytes.
func TestSendFileCaptionASCIISanitized(t *testing.T) {
	ctx := ctxWith(t, "sess-sf-ascii", "call-sf")

	raw, _ := json.Marshal(map[string]string{"path": "/workspace/report.pdf", "caption": "città è caffè — résumé"})
	res, err := (&SendFile{Router: boxDelivery(t, "report.pdf", "pdf bytes")}).Execute(ctx, raw)
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
	// The size gate runs over the STAGED copy, so the oversized bytes have to come out of the box.
	// stageBoxArtifact reads maxSendFileBytes+1 at most, which is exactly what the gate needs to
	// see to reject.
	ctx := ctxWith(t, "sess-sf-big", "call-sf")

	raw, _ := json.Marshal(map[string]string{"path": "/workspace/huge.bin"})
	res, err := (&SendFile{Router: boxDelivery(t, "huge.bin", strings.Repeat("x", maxSendFileBytes+1))}).
		Execute(ctx, raw)
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
	// A path the box cannot produce (missing file, or a directory) yields a tar stream with no
	// regular file, which stageBoxArtifact reports and the tool turns into file_unreadable.
	ctx := ctxWith(t, "sess-sf-bad", "call-sf")
	emptyTar := tarStream(t)
	router := usersandbox.NewSandboxRouter(
		routedBackend{tarBytes: emptyTar.Bytes()}, config.ProfileSingleUserHardened, unitSandboxCfg())

	raw, _ := json.Marshal(map[string]string{"path": "/workspace/does-not-exist.txt"})
	res, err := (&SendFile{Router: router}).Execute(ctx, raw)
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

// TestSendFileMissingPath: an empty path arg is a self-correctable error result,
// carrying the clearer guidance that names the canonical key + its aliases.
func TestSendFileMissingPath(t *testing.T) {
	ctx := ctxWith(t, "sess-sf-empty", "call-sf")
	res, err := (&SendFile{}).Execute(ctx, json.RawMessage(`{"caption":"x"}`)) // guard runs before the route
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("a missing path must NOT carry an artifact descriptor")
	}
	if !strings.Contains(res.Preview, "file_unreadable") {
		t.Fatalf("missing-path result must carry file_unreadable, got: %q", res.Preview)
	}
	for _, want := range []string{"no file path was provided", "file_path", "file"} {
		if !strings.Contains(res.Preview, want) {
			t.Fatalf("missing-path message must mention %q, got: %q", want, res.Preview)
		}
	}
}

// TestSendFileArgAliases pins the defense-in-depth alias decoding: the file path is
// accepted under the canonical `path` OR any of the aliases an LLM commonly reaches
// for (file_path, file, filepath, absolute_path). All resolve to sendFileArgs.Path.
func TestSendFileArgAliases(t *testing.T) {
	for _, key := range []string{"path", "file_path", "file", "filepath", "absolute_path"} {
		t.Run(key, func(t *testing.T) {
			var a sendFileArgs
			raw := json.RawMessage(`{"` + key + `":"/x","caption":"c"}`)
			if err := json.Unmarshal(raw, &a); err != nil {
				t.Fatalf("Unmarshal %s: %v", key, err)
			}
			if a.Path != "/x" {
				t.Fatalf("alias %q resolved Path = %q, want /x", key, a.Path)
			}
			if a.Caption != "c" {
				t.Fatalf("alias %q dropped caption, got %q", key, a.Caption)
			}
		})
	}

	// Precedence: canonical path wins over any alias when both are present.
	t.Run("path_precedence", func(t *testing.T) {
		var a sendFileArgs
		raw := json.RawMessage(`{"path":"/canon","file_path":"/alias","file":"/other"}`)
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if a.Path != "/canon" {
			t.Fatalf("precedence Path = %q, want /canon (canonical wins)", a.Path)
		}
	})
}
