package tools

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// document_index indexes a file that lives on the BOX's /workspace volume, which is NOT the
// volume the aura container mounts at the same name. These tests pin that direction daemon-free:
// the box path asked for, the fence applied to it, the fail-CLOSED deny when the box cannot be
// reached, and — the invisible one — that the staged host copy is gone on EVERY exit path. The
// live copy-out is the docker_integration tier's job, and that tier feeds zero coverage.

// indexBoxBase is a minimal usersandbox.Backend WITHOUT the artifact copy-out capability: the
// structural "this backend cannot do that" case, which must deny rather than read the host.
type indexBoxBase struct{ resolveErr error }

func (b indexBoxBase) Resolve(_ context.Context, spec usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	if b.resolveErr != nil {
		return usersandbox.BoxHandle{}, b.resolveErr
	}
	return usersandbox.BoxHandle{ContainerID: "c-" + spec.IdentityID, IdentityID: spec.IdentityID}, nil
}

func (indexBoxBase) Exec(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	return usersandbox.ExecResult{}, nil
}
func (indexBoxBase) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (indexBoxBase) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (indexBoxBase) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

// indexBox adds copy-out and records every BOX path it was asked for — which is how the tests
// prove the tool never resolves the caller's path against the aura container's filesystem.
type indexBox struct {
	indexBoxBase
	tarBytes  []byte
	copyErr   error
	copiedOut []string
}

func (b *indexBox) CopyArtifactsOut(_ context.Context, _ usersandbox.BoxHandle, boxPath string) (io.ReadCloser, error) {
	b.copiedOut = append(b.copiedOut, boxPath)
	if b.copyErr != nil {
		return nil, b.copyErr
	}
	return io.NopCloser(bytes.NewReader(b.tarBytes)), nil
}

func indexRouter(be usersandbox.Backend) *usersandbox.SandboxRouter {
	return usersandbox.NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxCfg())
}

func boxTar(t *testing.T, name, body string) *indexBox {
	t.Helper()
	return &indexBox{tarBytes: tarStream(t, tarEntry{name, tar.TypeReg, body}).Bytes()}
}

// indexCtx returns the tool-call context Execute needs (NewResult and the staging root both read
// it) plus the run dir, so a test can look for staging leftovers under it.
func indexCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	runDir := t.TempDir()
	return WithToolCallContext(t.Context(), "session", "toolcall", runDir, 4096), runDir
}

// assertNothingStaged fails if anything survives under the staging root ($AURA_RUN_DIR/tmp). The
// staged copy is a byte-for-byte duplicate of the indexed file — up to 50 MiB per call — and only
// a 24h sweeper would ever reclaim it, so a leak here shows up as a full disk, not as a failure.
func assertNothingStaged(t *testing.T, runDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(runDir, "tmp"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read staging root: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("staging root still holds %v after the call — the staged copy leaked", names)
	}
}

type fakeIndexer struct {
	called     bool
	calledPath string
	calledReq  assets.DocumentIngestRequest
	read       []byte
	readErr    error
	asset      assets.Asset
	err        error
}

func (f *fakeIndexer) IngestDocument(_ context.Context, req assets.DocumentIngestRequest) (assets.Asset, error) {
	f.called = true
	f.calledReq = req
	if named, ok := req.Reader.(interface{ Name() string }); ok {
		f.calledPath = named.Name()
	}
	f.read, f.readErr = io.ReadAll(req.Reader)
	if f.err != nil {
		return assets.Asset{}, f.err
	}
	return f.asset, nil
}

// TestDocumentIndex_StagesTheBoxFileForIngest is the fix itself: a workspace-relative path
// resolves to a BOX path, the artifact is copied out of the box, and the ingest is handed a HOST
// path holding those bytes — never the box path, which the aura container cannot open.
func TestDocumentIndex_StagesTheBoxFileForIngest(t *testing.T) {
	ctx, runDir := indexCtx(t)
	be := boxTar(t, "r.docx", "DOCXBYTES")
	fi := &fakeIndexer{asset: assets.Asset{ID: "asset-1", FileName: "r.docx", Status: assets.StatusAccepted}}

	res, err := (&DocumentIndex{Indexer: fi, Router: indexRouter(be)}).
		Execute(ctx, json.RawMessage(`{"path":"artifacts/r.docx"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"/workspace/artifacts/r.docx"}; !slices.Equal(be.copiedOut, want) {
		t.Fatalf("box was asked for %v, want %v (a relative path joins onto the BOX /workspace)", be.copiedOut, want)
	}
	if !fi.called {
		t.Fatal("IngestDocument was not called")
	}
	if fi.readErr != nil || string(fi.read) != "DOCXBYTES" {
		t.Fatalf("ingest read %q (err %v), want the box artifact's bytes", fi.read, fi.readErr)
	}
	// The catalog records the BOX identity of the file, not the staging path, which is gone by
	// the time Execute returns. FileName also drives the supported-type gate and the card reader.
	if fi.calledReq.FileName != "r.docx" {
		t.Fatalf("FileName = %q, want the box basename r.docx", fi.calledReq.FileName)
	}
	if fi.calledReq.SourceRef != documentSourceFingerprint("/workspace/artifacts/r.docx") || strings.Contains(fi.calledReq.SourceRef, "/workspace") {
		t.Fatalf("SourceRef = %q, want an opaque stable fingerprint", fi.calledReq.SourceRef)
	}
	if fi.calledReq.SourceKind != assets.SourceWorkspace {
		t.Fatalf("SourceKind = %q, want workspace", fi.calledReq.SourceKind)
	}
	if fi.calledReq.IdentityID == "" {
		t.Fatal("expected a non-empty owning identity (ownerFromContext) on the ingest request")
	}
	if !strings.Contains(res.Preview, "asset-1") {
		t.Fatalf("result must report the accepted asset id, got %q", res.Preview)
	}
	assertNothingStaged(t, runDir)
}

// A title is display metadata; the stable source identity remains the path fingerprint.
func TestDocumentIndex_TitleIsDisplayMetadata(t *testing.T) {
	ctx, runDir := indexCtx(t)
	fi := &fakeIndexer{asset: assets.Asset{ID: "asset-2", FileName: "r.docx", Status: assets.StatusAccepted}}
	if _, err := (&DocumentIndex{Indexer: fi, Router: indexRouter(boxTar(t, "r.docx", "X"))}).
		Execute(ctx, json.RawMessage(`{"path":"/workspace/r.docx","title":"Q3 report"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fi.calledReq.Title != "Q3 report" {
		t.Fatalf("Title = %q, want the caller's title", fi.calledReq.Title)
	}
	assertNothingStaged(t, runDir)
}

func TestDocumentIndex_EnforcesConfiguredDocumentLimitBeforeIngest(t *testing.T) {
	ctx, runDir := indexCtx(t)
	fi := &fakeIndexer{}
	_, err := (&DocumentIndex{
		Indexer:  fi,
		Router:   indexRouter(boxTar(t, "oversize.txt", "12345")),
		MaxBytes: 4,
	}).Execute(ctx, json.RawMessage(`{"path":"oversize.txt"}`))
	if err == nil || !strings.Contains(err.Error(), "4-byte") {
		t.Fatalf("err = %v, want configured 4-byte limit", err)
	}
	if fi.called {
		t.Fatal("an oversized artifact must be rejected before canonical ingest")
	}
	assertNothingStaged(t, runDir)
}

// TestDocumentIndex_FencesToTheBoxWorkspace pins the traversal + workspace fence, now applied to
// the BOX path: ingest-into-searchable-memory is a write-class action, so it stays confined to
// /workspace. The fence runs BEFORE the copy-out, so a rejected path never reaches the box at all.
func TestDocumentIndex_FencesToTheBoxWorkspace(t *testing.T) {
	for _, outside := range []string{"/etc/passwd", "/skills/calc/calc.py", "/workspace/../etc/shadow", "../escape.txt", "/"} {
		t.Run(outside, func(t *testing.T) {
			ctx, runDir := indexCtx(t)
			be := boxTar(t, "leak.txt", "leaked")
			fi := &fakeIndexer{}
			raw, err := json.Marshal(map[string]string{"path": outside})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			if _, err := (&DocumentIndex{Indexer: fi, Router: indexRouter(be)}).Execute(ctx, raw); err == nil {
				t.Fatal("expected rejection for a path outside the box workspace")
			}
			if len(be.copiedOut) != 0 {
				t.Fatalf("box was asked for %v; the fence must reject before any copy-out", be.copiedOut)
			}
			if fi.calledPath != "" {
				t.Fatal("IngestPath must not run for an out-of-workspace path")
			}
			assertNothingStaged(t, runDir)
		})
	}
}

// "~" is refused rather than expanded: the aura container's home is a different filesystem from
// the box's, and there is no shell anywhere on this path to expand it (boxPathArg).
func TestDocumentIndex_RefusesTildePaths(t *testing.T) {
	ctx, _ := indexCtx(t)
	fi := &fakeIndexer{}
	be := boxTar(t, "secret.txt", "x")
	_, err := (&DocumentIndex{Indexer: fi, Router: indexRouter(be)}).
		Execute(ctx, json.RawMessage(`{"path":"~/secret.txt"}`))
	if err == nil {
		t.Fatal("expected a tilde path to be refused")
	}
	if !strings.Contains(err.Error(), "/workspace") {
		t.Fatalf("the refusal must point at /workspace, got %v", err)
	}
	if len(be.copiedOut) != 0 || fi.calledPath != "" {
		t.Fatal("a tilde path must reach neither the box nor the ingest")
	}
}

// TestDocumentIndex_DeniesWhenTheBoxIsUnavailable is the containment case, in all three shapes an
// unreachable box takes. Every one of them DENIES with the fail-CLOSED result — there is no
// host-filesystem arm left to read the file from (D-09/GATE-01).
func TestDocumentIndex_DeniesWhenTheBoxIsUnavailable(t *testing.T) {
	cases := map[string]*DocumentIndex{}
	fis := map[string]*fakeIndexer{}
	for name, be := range map[string]usersandbox.Backend{
		"resolve fails":        &indexBox{indexBoxBase: indexBoxBase{resolveErr: errors.New("dockerd down")}},
		"copy-out fails":       &indexBox{copyErr: errors.New("no such container")},
		"copy-out unsupported": indexBoxBase{},
	} {
		fi := &fakeIndexer{}
		fis[name] = fi
		cases[name] = &DocumentIndex{Indexer: fi, Router: indexRouter(be)}
	}
	// A nil router is the same denial point: NewSandboxRouter is the only constructor that
	// allocates its maps, so Route refuses a zero-value/absent router rather than panicking.
	nilRouterIndexer := &fakeIndexer{}
	fis["no router"] = nilRouterIndexer
	cases["no router"] = &DocumentIndex{Indexer: nilRouterIndexer}

	for name, tool := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, runDir := indexCtx(t)
			res, err := tool.Execute(ctx, json.RawMessage(`{"path":"r.docx"}`))
			if err != nil {
				t.Fatalf("an unreachable box is a DENY result, not a Go error: %v", err)
			}
			if !strings.Contains(res.Preview, "sandbox_unavailable") {
				t.Fatalf("want a sandbox_unavailable deny, got %q", res.Preview)
			}
			if fis[name].calledPath != "" {
				t.Fatalf("IngestPath ran with %q — an unreachable box must never fall back to a host read",
					fis[name].calledPath)
			}
			assertNothingStaged(t, runDir)
		})
	}
}

// TestDocumentIndex_RemovesTheStagedCopyWhenIngestFails is the error-path cleanup: the ingest ran,
// so the staged copy exists, and a failure must still take it with it.
func TestDocumentIndex_RemovesTheStagedCopyWhenIngestFails(t *testing.T) {
	ctx, runDir := indexCtx(t)
	fi := &fakeIndexer{err: errors.New("job store is down")}

	_, err := (&DocumentIndex{Indexer: fi, Router: indexRouter(boxTar(t, "r.docx", "DOCXBYTES"))}).
		Execute(ctx, json.RawMessage(`{"path":"r.docx"}`))
	if err == nil {
		t.Fatal("expected the ingest failure to surface")
	}
	if fi.calledPath == "" {
		t.Fatal("the ingest must have been attempted for this to be testing its cleanup")
	}
	if _, serr := os.Stat(fi.calledPath); !os.IsNotExist(serr) {
		t.Fatalf("staged copy %q survived a failed ingest (stat err %v)", fi.calledPath, serr)
	}
	assertNothingStaged(t, runDir)
}

// A stream carrying no regular file (a missing path, or a directory) is the model's own problem
// and reads as one — and it leaves no half-made staging dir behind either.
func TestDocumentIndex_LeavesNothingBehindWhenStagingFails(t *testing.T) {
	ctx, runDir := indexCtx(t)
	fi := &fakeIndexer{}
	be := &indexBox{tarBytes: tarStream(t).Bytes()}

	_, err := (&DocumentIndex{Indexer: fi, Router: indexRouter(be)}).
		Execute(ctx, json.RawMessage(`{"path":"missing.docx"}`))
	if err == nil {
		t.Fatal("expected an error for an artifact the box could not produce")
	}
	if !strings.Contains(err.Error(), "/workspace/missing.docx") {
		t.Fatalf("the error must name the box path the model asked for, got %v", err)
	}
	if fi.calledPath != "" {
		t.Fatal("nothing may be ingested when the artifact could not be staged")
	}
	assertNothingStaged(t, runDir)
}

// TestVetStagedForIngest covers the host-side invariants on the staged copy directly — the size
// bound that stops an agent making the aura container buffer an unbounded file out of its box,
// and the symlink fence over the staging dir.
func TestVetStagedForIngest(t *testing.T) {
	t.Run("accepts a regular file inside the staging dir", func(t *testing.T) {
		dir := t.TempDir()
		staged := filepath.Join(dir, "r.docx")
		if err := os.WriteFile(staged, []byte("ok"), 0o600); err != nil {
			t.Fatal(err)
		}
		resolved, err := vetStagedForIngest(dir, staged, "/workspace/r.docx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Base(resolved) != "r.docx" {
			t.Fatalf("resolved = %q, want the staged file", resolved)
		}
	})

	t.Run("refuses a file over the stage bound", func(t *testing.T) {
		// stageBoxArtifact reads at most maxSendFileBytes+1, so anything ABOVE the bound arrives
		// truncated. The gate has to sit exactly there: cataloging a short copy as a whole
		// document is worse than refusing it, because the agent then computes on it.
		dir := t.TempDir()
		staged := filepath.Join(dir, "huge.bin")
		f, err := os.Create(staged)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(maxIndexedFileBytes + 1); err != nil {
			_ = f.Close()
			t.Skipf("cannot size a %d-byte file on this host: %v", maxIndexedFileBytes+1, err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = vetStagedForIngest(dir, staged, "/workspace/huge.bin")
		if err == nil {
			t.Fatal("expected an over-cap staged copy to be refused")
		}
		if !strings.Contains(err.Error(), "limit") {
			t.Fatalf("the refusal must name the limit, got %v", err)
		}
	})

	t.Run("refuses a symlink escaping the staging dir", func(t *testing.T) {
		dir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outside, []byte("host secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		staged := filepath.Join(dir, "link.txt")
		if err := os.Symlink(outside, staged); err != nil {
			t.Skipf("symlinks unavailable on this host: %v", err)
		}
		if _, err := vetStagedForIngest(dir, staged, "/workspace/link.txt"); err == nil {
			t.Fatal("a staged entry resolving outside its staging dir must not be ingested")
		}
	})

	t.Run("refuses a missing staged file", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := vetStagedForIngest(dir, filepath.Join(dir, "gone.docx"), "/workspace/gone.docx"); err == nil {
			t.Fatal("expected an error for a staged file that is not there")
		}
	})
}

func TestDocumentIndex_SpecContract(t *testing.T) {
	spec := (&DocumentIndex{}).Spec()
	if spec.Name != "document_index" {
		t.Errorf("Name = %q, want document_index", spec.Name)
	}
	if !spec.Deferred {
		t.Error("document_index must be Deferred (occasional action; the <documents> prompt block names it for tool_search)")
	}
	if !strings.Contains(spec.Description, "document_search") || !strings.Contains(spec.Description, "/workspace") {
		t.Error("Spec description must tie document_index to document_search and /workspace")
	}
	// The description tells the agent to index files it produced. That is only true if the
	// /workspace it names is the one its own tools write to — so the text must say which
	// /workspace it means, or it is the same promise that could not be kept before.
	for _, needle := range []string{"shell_exec", "fs_write", "workspace container"} {
		if !strings.Contains(spec.Description, needle) {
			t.Errorf("Spec description must name %q so the agent knows WHICH /workspace is indexed", needle)
		}
	}
}

func TestDocumentIndex_RequiresPath(t *testing.T) {
	tool := &DocumentIndex{Indexer: &fakeIndexer{}, Router: indexRouter(&indexBox{})}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"   "}`)); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestDocumentIndex_NilBackendErrors(t *testing.T) {
	tool := &DocumentIndex{Router: indexRouter(&indexBox{})}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"a.txt"}`)); err == nil {
		t.Fatal("expected error when indexer is not configured")
	}
}

func TestDescriptions_CrossReferenceDocumentIndex(t *testing.T) {
	ds := (&DocumentSearch{}).Spec().Description
	if !strings.Contains(ds, "document_index") || !strings.Contains(ds, "/workspace") {
		t.Errorf("document_search description must point workspace-file questions at fs_* / document_index")
	}
	sf := (&SendFile{}).Spec().Description
	if !strings.Contains(sf, "document_index") {
		t.Errorf("send_file description must note delivered files are not searchable until document_index")
	}
}
