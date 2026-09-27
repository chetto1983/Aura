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
	"time"

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
	startFor time.Duration // a cold start: the server answers only after this long
	echoTool string        // when set, one more tool, answering with the arguments it received

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
	if b.echoTool != "" {
		server.AddTool(&sdkmcp.Tool{Name: b.echoTool, InputSchema: map[string]any{"type": "object"}},
			func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(req.Params.Arguments)}}}, nil
			})
	}
	go func() {
		defer close(p.done)
		time.Sleep(b.startFor)
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

// A box server's cold start fetches it into the identity's caches, and gets its init timeout
// even when the mount's own budget is shorter; a start slower than the init timeout still fails.
func TestBoxServerFirstStartGetsItsInitTimeout(t *testing.T) {
	mount := func(startFor time.Duration) error {
		boxes := &identityBoxes{startFor: startFor}
		server := boxServer
		server.Runtime.InitTimeoutSec = 1
		handshakeCtx, cancel := context.WithTimeout(identityctx.WithIdentityID(t.Context(), "identity-a"), 200*time.Millisecond)
		defer cancel()
		closer, _, _, err := MountManagedServerWithOptions(t.Context(), handshakeCtx, tools.NewRegistry(), "fetch", server, MountOptions{Box: boxes})
		if err == nil {
			_ = closer()
		}
		boxes.mu.Lock()
		procs := slices.Clone(boxes.procs)
		boxes.mu.Unlock()
		for _, p := range procs {
			<-p.done
		}
		mcp.WaitForAbandonedCalls()
		return err
	}
	if err := mount(400 * time.Millisecond); err != nil {
		t.Fatalf("a 0.4 s cold start under a 0.2 s mount budget and a 1 s init timeout: %v", err)
	}
	if err := mount(1500 * time.Millisecond); err == nil {
		t.Fatal("a 1.5 s start past its 1 s init timeout mounted anyway")
	}
}

func TestWithFirstStartOnlyEverWidensABoundedBudget(t *testing.T) {
	p := &identitySessionPool{firstStart: time.Second}
	short, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	got, release := p.withFirstStart(short)
	defer release()
	if d, _ := got.Deadline(); time.Until(d) < 900*time.Millisecond {
		t.Fatalf("a 10 ms budget was not widened to the 1 s first start: %v left", time.Until(d))
	}
	long, cancelLong := context.WithTimeout(t.Context(), time.Minute)
	defer cancelLong()
	if got, _ := p.withFirstStart(long); got != long {
		t.Fatal("a budget already longer than the first start was replaced")
	}
	if got, _ := p.withFirstStart(t.Context()); got != t.Context() {
		t.Fatal("an unbounded context was given a deadline")
	}
	if got, _ := (&identitySessionPool{}).withFirstStart(short); got != short {
		t.Fatal("an OAuth pool, with no first start, changed the budget")
	}
}
