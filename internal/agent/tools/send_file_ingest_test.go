package tools

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// fakeDeliverer records the one IngestAgentDelivery a successful delivery makes and can be told
// to fail (the D-02 Put-error degrade). It is the daemon-free AssetDeliverer double the send_file
// ingest/degrade tests inject in place of the cmd/aura *assets.Service adapter.
type fakeDeliverer struct {
	err   error
	retID string

	calls     int
	gotID     string
	gotThread string
	gotPath   string
	gotName   string
	gotMIME   string
	gotSize   int64
}

func (f *fakeDeliverer) IngestAgentDelivery(_ context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (string, error) {
	f.calls++
	f.gotID, f.gotThread, f.gotPath, f.gotName, f.gotMIME, f.gotSize = identityID, threadID, hostPath, filename, mimeType, size
	if f.err != nil {
		return "", f.err
	}
	if f.retID == "" {
		return "asset-default", nil
	}
	return f.retID, nil
}

func sfArgs(t *testing.T, path, caption string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"path": path, "caption": caption})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}

func artifactMap(t *testing.T, res ToolResult) map[string]any {
	t.Helper()
	if res.Meta == nil {
		t.Fatalf("ToolResult.Meta is nil, want an artifact descriptor; preview=%q", res.Preview)
	}
	m, ok := (*res.Meta)["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("Meta[artifact] = %#v, want map[string]any", (*res.Meta)["artifact"])
	}
	return m
}

// boxDelivery builds the router a send_file call now needs: a daemon-free backend whose
// CopyArtifactsOut replays a one-file tar, so Execute stages that file out of the box into the
// run dir and delivers the staged copy. There is no host-file delivery arm to test against any
// more — /workspace/<name> is the only kind of path send_file accepts.
func boxDelivery(t *testing.T, name, body string) *usersandbox.SandboxRouter {
	t.Helper()
	tarBuf := tarStream(t, tarEntry{name, tar.TypeReg, body})
	return usersandbox.NewSandboxRouter(
		routedBackend{tarBytes: tarBuf.Bytes()}, config.ProfileSingleUserHardened, unitSandboxCfg())
}

// TestSendFile_IngestSuccess is the happy path: an authenticated identity with a non-empty thread
// id (sessionID) + a wired AssetDeliverer produces a descriptor carrying ALL keys
// (path/filename/caption/tool_call_id/size_bytes + the ingest-derived asset_id/mime_type), and the
// fake recorded exactly one IngestAgentDelivery with the resolved identity, thread, and host path.
func TestSendFile_IngestSuccess(t *testing.T) {
	fake := &fakeDeliverer{retID: "asset-xyz"}
	ctx := ctxWithIdentity(t, "conv-1", "call-1", "id-42")

	res, err := (&SendFile{Router: boxDelivery(t, "results.xlsx", "small spreadsheet bytes"), Assets: fake}).
		Execute(ctx, sfArgs(t, "/workspace/results.xlsx", "results"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	art := artifactMap(t, res)

	if got := art["asset_id"]; got != "asset-xyz" {
		t.Fatalf("asset_id = %v, want asset-xyz", got)
	}
	mt, _ := art["mime_type"].(string)
	if mt == "" {
		t.Fatalf("mime_type missing on success: %#v", art)
	}
	if got := art["size_bytes"]; got != int64(len("small spreadsheet bytes")) {
		t.Fatalf("size_bytes = %v, want %d", got, len("small spreadsheet bytes"))
	}
	if got := art["tool_call_id"]; got != "call-1" {
		t.Fatalf("tool_call_id = %v, want call-1", got)
	}
	if art["path"] == nil || art["filename"] != "results.xlsx" {
		t.Fatalf("path/filename missing: %#v", art)
	}

	if fake.calls != 1 {
		t.Fatalf("IngestAgentDelivery calls = %d, want 1", fake.calls)
	}
	if fake.gotID != "id-42" {
		t.Fatalf("ingest identity = %q, want id-42", fake.gotID)
	}
	if fake.gotThread != "conv-1" {
		t.Fatalf("ingest thread = %q, want conv-1 (sessionID == ConvID)", fake.gotThread)
	}
	if fake.gotPath != art["path"] {
		t.Fatalf("ingest hostPath = %q, want the descriptor path %v", fake.gotPath, art["path"])
	}
	if fake.gotSize != int64(len("small spreadsheet bytes")) {
		t.Fatalf("ingest size = %d, want %d", fake.gotSize, len("small spreadsheet bytes"))
	}
}

// TestSendFile_DescriptorFields pins the full success field-set + that the descriptor stays
// channel-agnostic (still carries path, so Telegram parity holds).
func TestSendFile_DescriptorFields(t *testing.T) {
	ctx := ctxWithIdentity(t, "conv-f", "call-f", "id-f")

	res, err := (&SendFile{Router: boxDelivery(t, "report.pdf", "pdf"), Assets: &fakeDeliverer{retID: "a1"}}).
		Execute(ctx, sfArgs(t, "/workspace/report.pdf", "cap"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	art := artifactMap(t, res)
	for _, k := range []string{"path", "filename", "caption", "tool_call_id", "size_bytes", "asset_id", "mime_type"} {
		if _, ok := art[k]; !ok {
			t.Fatalf("success descriptor missing %q: %#v", k, art)
		}
	}
}

// TestSendFile_DegradeMatrix is the four-way degrade matrix (D-02/D-05): each miss keeps a
// {path,filename,caption,tool_call_id,size_bytes} descriptor with NO asset_id and NO mime_type,
// and NEVER surfaces a Go error. The correlation key (tool_call_id) + size_bytes ride on degrade
// so 37A-04's reducer can still attach the render-only "delivery unavailable" card.
func TestSendFile_DegradeMatrix(t *testing.T) {
	body := "degrade-bytes"

	cases := []struct {
		name  string
		build func(t *testing.T, router *usersandbox.SandboxRouter) (*SendFile, context.Context)
	}{
		{
			name: "nil Assets",
			build: func(t *testing.T, router *usersandbox.SandboxRouter) (*SendFile, context.Context) {
				return &SendFile{Router: router}, ctxWithIdentity(t, "conv-a", "call-a", "id-a")
			},
		},
		{
			name: "empty identity",
			build: func(t *testing.T, router *usersandbox.SandboxRouter) (*SendFile, context.Context) {
				// ctxWith carries a tool-call ctx (sessionID + toolCallID) but NO identity principal.
				return &SendFile{Router: router, Assets: &fakeDeliverer{retID: "nope"}}, ctxWith(t, "conv-b", "call-b")
			},
		},
		{
			name: "empty thread id",
			build: func(t *testing.T, router *usersandbox.SandboxRouter) (*SendFile, context.Context) {
				// A tool-call ctx with an EMPTY sessionID (no ConvID) + a scoped identity ⇒ D-05 degrade.
				ctx := WithToolCallContext(context.Background(), "", "call-c", t.TempDir(), testCap)
				ctx = identityctx.WithIdentityID(ctx, "id-c")
				return &SendFile{Router: router, Assets: &fakeDeliverer{retID: "nope"}}, ctx
			},
		},
		{
			name: "ingest error",
			build: func(t *testing.T, router *usersandbox.SandboxRouter) (*SendFile, context.Context) {
				return &SendFile{Router: router, Assets: &fakeDeliverer{err: context.DeadlineExceeded}}, ctxWithIdentity(t, "conv-d", "call-d", "id-d")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sf, ctx := tc.build(t, boxDelivery(t, "deg.bin", body))

			res, err := sf.Execute(ctx, sfArgs(t, "/workspace/deg.bin", "cap"))
			if err != nil {
				t.Fatalf("degrade must NOT error the turn: %v", err)
			}
			art := artifactMap(t, res)

			if _, ok := art["asset_id"]; ok {
				t.Fatalf("degraded descriptor MUST NOT carry asset_id: %#v", art)
			}
			if _, ok := art["mime_type"]; ok {
				t.Fatalf("degraded descriptor MUST NOT carry mime_type: %#v", art)
			}
			if art["path"] == nil {
				t.Fatalf("degraded descriptor must keep path (Telegram parity): %#v", art)
			}
			if _, ok := art["tool_call_id"]; !ok {
				t.Fatalf("degraded descriptor MUST keep tool_call_id (web-card reachability): %#v", art)
			}
			if got := art["size_bytes"]; got != int64(len(body)) {
				t.Fatalf("degraded descriptor must keep size_bytes = %d, got %v", len(body), got)
			}
		})
	}
}

// routedBackend is a daemon-free usersandbox.Backend that also satisfies the structural
// artifactCopier (CopyArtifactsOut) send_file's routed branch resolves — so deliverFromBox can be
// driven end-to-end without a Docker daemon. CopyArtifactsOut replays an in-memory tar (the
// CopyArtifactsOut wire shape) carrying one regular file.
type routedBackend struct{ tarBytes []byte }

func (routedBackend) Resolve(_ context.Context, spec usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	return usersandbox.BoxHandle{ContainerID: "c-" + spec.IdentityID, IdentityID: spec.IdentityID}, nil
}
func (routedBackend) Exec(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	return usersandbox.ExecResult{}, nil
}
func (routedBackend) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (routedBackend) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (routedBackend) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func (b routedBackend) CopyArtifactsOut(_ context.Context, _ usersandbox.BoxHandle, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.tarBytes)), nil
}

// TestSendFile_DeniesWhenTheBoxIsUnreachable is the containment case for delivery: there is no
// host-file arm left, so an unresolvable box DENIES and no descriptor is emitted. (This replaces
// TestSendFile_IngestBothTails, which asserted a host tail and a routed tail both ingested; there
// is one tail now, and every other test in this file exercises it.)
func TestSendFile_DeniesWhenTheBoxIsUnreachable(t *testing.T) {
	fake := &fakeDeliverer{retID: "asset-never"}
	res, err := (&SendFile{Router: routerWith(&fakeBox{resolveE: errors.New("dockerd down")}), Assets: fake}).
		Execute(ctxWithIdentity(t, "conv-x", "call-x", "id-x"), sfArgs(t, "/workspace/report.xlsx", ""))
	if err != nil {
		t.Fatalf("an unreachable box is a DENY result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "sandbox_unavailable") {
		t.Fatalf("want a sandbox_unavailable deny, got: %q", res.Preview)
	}
	if res.Meta != nil {
		t.Fatalf("a denied delivery must emit no artifact descriptor: %#v", res.Meta)
	}
	if fake.calls != 0 {
		t.Fatalf("a denied delivery must not ingest: %d calls", fake.calls)
	}
}

// unitSandboxCfg is a non-degenerate SandboxConfig for the routed-tail router (the fake backend
// ignores the spec; the config only needs to be structurally valid for specFor).
func unitSandboxCfg() config.SandboxConfig {
	return config.SandboxConfig{IdleTTLSec: 1800, CPULimit: 2, MemoryLimit: 2 << 30, PidsLimit: 512, Image: "aura-sandbox:latest"}
}
