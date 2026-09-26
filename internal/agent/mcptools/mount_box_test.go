package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// identityBoxes stands in for the sandbox router: every start runs a real SDK server over the
// exec's pipes, in the "box" of the identity the context carries, and its one tool answers
// with that identity — so a call that reached the wrong box would say so.
type identityBoxes struct {
	mu     sync.Mutex
	starts []string
	procs  []*boxProc
}

type boxProc struct {
	stdin io.ReadCloser
	done  chan struct{}
}

func (p *boxProc) Wait() (int, error) { <-p.done; return 0, nil }
func (p *boxProc) Kill()              { _ = p.stdin.Close() }
func (p *boxProc) Touch()             {}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (b *identityBoxes) StartStdio(ctx context.Context, _ string, _, _ []string, stdin io.ReadCloser, stdout io.Writer) (mcp.BoxProcess, error) {
	owner := identityctx.IdentityID(ctx)
	p := &boxProc{stdin: stdin, done: make(chan struct{})}
	b.mu.Lock()
	b.starts = append(b.starts, owner)
	b.procs = append(b.procs, p)
	b.mu.Unlock()

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "box", Version: "0.0.1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "whoami", InputSchema: map[string]any{"type": "object"},
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: owner}}}, nil
		})
	go func() {
		defer close(p.done)
		ss, err := server.Connect(context.Background(), &sdkmcp.IOTransport{Reader: stdin, Writer: nopWriteCloser{stdout}}, nil)
		if err == nil {
			_ = ss.Wait()
		}
	}()
	return p, nil
}

var boxServer = mcp.ManagedServer{Command: "agent-browser", Args: []string{"mcp"}, Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox}}

func TestBoxServerRunsInEachIdentitysOwnBox(t *testing.T) {
	boxes := &identityBoxes{}
	reg := tools.NewRegistry()
	handshakeCtx := identityctx.WithIdentityID(t.Context(), "identity-a")
	closer, names, _, err := MountManagedServerWithOptions(t.Context(), handshakeCtx, reg, "browser", boxServer, MountOptions{Box: boxes})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() {
		_ = closer()
		for _, p := range boxes.procs {
			<-p.done
		}
	})
	if !slices.Equal(names, []string{"browser__whoami"}) {
		t.Fatalf("mounted %q", names)
	}
	tool, _ := reg.Get(names[0])

	for _, identity := range []string{"identity-a", "identity-b", "identity-a"} {
		ctx := tools.WithToolCallContext(identityctx.WithIdentityID(t.Context(), identity), "sess", "tc", t.TempDir(), 2048)
		res, err := tool.Execute(ctx, json.RawMessage(`{}`))
		if err != nil || !strings.Contains(res.Preview, identity) {
			t.Fatalf("as %s: %+v, %v — the call did not run in its own box", identity, res, err)
		}
	}
	if !slices.Equal(boxes.starts, []string{"identity-a", "identity-b"}) {
		t.Fatalf("box starts = %q, want one per identity, the mount's reused", boxes.starts)
	}
	if _, err := tool.Execute(t.Context(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("a call with no identity reached somebody's box")
	}
}

func TestBoxServerDoesNotMountWithoutASandbox(t *testing.T) {
	reg := tools.NewRegistry()
	handshakeCtx := identityctx.WithIdentityID(t.Context(), "identity-a")
	_, _, _, err := MountManagedServerWithOptions(t.Context(), handshakeCtx, reg, "browser", boxServer, MountOptions{})
	if !errors.Is(err, mcp.ErrNoBox) {
		t.Fatalf("mount without a sandbox = %v, want ErrNoBox", err)
	}
	if reg.HasPrefix("browser__") {
		t.Fatal("a box server that could not start left tools behind")
	}
}

func TestBrowserRecipePolicyIsIdentityScopedAndGradedByItsTable(t *testing.T) {
	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	policy := managedBridgePolicy(recipe.Server)
	if !policy.identityScoped || policy.recipeSource != mcpmanager.BrowserRecipeSource {
		t.Fatalf("browser policy = %+v, want identity-scoped and graded by its recipe table", policy)
	}
	click := &sdkmcp.Tool{Name: "agent_browser_click", Annotations: &sdkmcp.ToolAnnotations{}}
	if mutating, destructive := classifyToolRisk(policy, click); !mutating || destructive {
		t.Fatalf("click graded mutating=%v destructive=%v, want a reversible write", mutating, destructive)
	}
}
