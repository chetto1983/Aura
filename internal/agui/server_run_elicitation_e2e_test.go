package agui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/runner"
)

// server_run_elicitation_e2e_test.go drives a real runner, a real MCP mount and
// the real detached handler: a server's form reaches the SSE stream, the answer
// is POSTed, and the server greets the name it received.

func formSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	}
}

func greet(reply *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	text := reply.Action
	if reply.Action == elicit.ActionAccept {
		text = fmt.Sprintf("hello %v", reply.Content["name"])
	}
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}}
}

// formServer serves one tool, ask_name, over streamable HTTP. Classic narrows the
// server to 2025-11-25, the last protocol that allows elicitation/create during a
// call (go-sdk@v1.8.0 mcp/server.go:1619-1627).
func formServer(t *testing.T, classic bool) string {
	t.Helper()
	var opts *sdkmcp.ServerOptions
	if classic {
		opts = &sdkmcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}}
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "forms", Version: "0.0.1"}, opts)
	tool := &sdkmcp.Tool{Name: "ask_name", Description: "Asks the operator for a name.", InputSchema: map[string]any{"type": "object"}}
	server.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if classic {
			reply, err := req.Session.Elicit(ctx, &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: formSchema()})
			if err != nil {
				return nil, err
			}
			return greet(reply), nil
		}
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return greet(reply), nil
		}
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: formSchema()},
		}}, nil
	})
	ts := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(func() {
		for session := range server.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return ts.URL
}

// unreachableFallback fails the test if a detached cockpit run's form ever falls
// back to decline-and-surface.
type unreachableFallback struct{ t *testing.T }

func (f unreachableFallback) AskOperator(context.Context, elicit.Question) (string, map[string]any, error) {
	f.t.Errorf("the fallback consent was asked: a detached run must be asked through its own asker")
	return elicit.ActionDecline, nil, nil
}

// mountForms mounts the fixture the way production mounts a managed server.
func mountForms(t *testing.T, reg *tools.Registry, url string) string {
	t.Helper()
	server := mcp.ManagedServer{URL: url, Type: mcp.ServerTypeStreamableHTTP, Env: []string{"MCP_OAUTH_DISABLED=true"}}
	closer, names, _, err := mcptools.MountManagedServerWithOptions(context.Background(), context.Background(), reg, "forms", server,
		mcptools.MountOptions{Egress: mcp.RuntimeEgressPolicy(false, server), Elicitation: unreachableFallback{t}})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { _ = closer() })
	if len(names) != 1 {
		t.Fatalf("mounted %v, want one tool", names)
	}
	// The process grants only maxAlwaysLoadedMCPSlots = 2 always-loaded slots
	// (mcptools bridge_deferral.go grantLoadedSlot), and each subtest spends one, so
	// a rerun would find the tool deferred. The form is under test, not the
	// deferral: load the tool as the memory capture test does.
	if tool, ok := reg.Get(names[0]); ok && tool.Spec().Deferred {
		reg.Adopt([]tools.Tool{alwaysLoadedTool{tool}})
	}
	return names[0]
}

type alwaysLoadedTool struct{ tools.Tool }

func (t alwaysLoadedTool) Spec() tools.Spec {
	spec := t.Tool.Spec()
	spec.Deferred = false
	return spec
}

// newRealFormRunner is newRealSteerRunner without the steer inbox and with a
// short wallclock, over a registry the test has already mounted into. PreviewCap
// keeps the server's reply inline: at 0 every byte spills to a sidecar and the
// model sees only a read_tool_output pointer.
func newRealFormRunner(t *testing.T, client llm.Client, reg *tools.Registry, wallclockSec int) (*runner.Runner, *steerE2EConvStore) {
	t.Helper()
	conv := newSteerE2EConvStore()
	r := runner.New(runner.Deps{
		PreviewCap:      2048,
		RunDir:          t.TempDir(),
		Conv:            conv,
		Pause:           steerE2EPauseStore{},
		ApprovalExpiry:  steerE2EPauseStore{},
		Identity:        steerE2EIdentityStore{},
		CacheMetrics:    steerE2ECacheMetricStore{},
		ToolInvocations: steerE2EToolInvocationStore{},
		Client:          agenttest.TitleClient{Main: client, Title: agenttest.NewFakeClient(agenttest.TextChunks("stop", "Form test conversation"))},
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768, LoopMaxWallclockSec: wallclockSec},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
	})
	return r, conv
}

// sseData streams an SSE body's data payloads until the body ends.
func sseData(body io.Reader) <-chan string {
	out := make(chan string, 1024)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				out <- data
			}
		}
	}()
	return out
}

func awaitQuestion(t *testing.T, frames <-chan string) elicitationFrame {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				t.Fatal("the stream ended before the form arrived")
			}
			var frame struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			}
			if json.Unmarshal([]byte(data), &frame) != nil || frame.Name != ElicitationEventName {
				continue
			}
			var q elicitationFrame
			if err := json.Unmarshal(frame.Value, &q); err != nil {
				t.Fatalf("decode %s: %v", ElicitationEventName, err)
			}
			return q
		case <-timeout:
			t.Fatalf("no %s frame within 10s", ElicitationEventName)
		}
	}
}

func drainFrames(t *testing.T, frames <-chan string) string {
	t.Helper()
	var b strings.Builder
	timeout := time.After(20 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return b.String()
			}
			b.WriteString(data + "\n")
		case <-timeout:
			t.Fatalf("the run did not end within 20s; so far:\n%s", b.String())
		}
	}
}

// Review Focus 1 end to end, and Review Focus 4's replay. The operator takes 3 s,
// over a 1 s MCP call bound and a 2 s run wallclock.
func TestDetachedRunAnswersAnMCPFormWhileBothClocksStop(t *testing.T) {
	for _, mode := range []struct {
		name    string
		classic bool
	}{{"mrtr", false}, {"classic", true}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("AURA_MCP_CALL_TIMEOUT_SEC", "1")
			reg := tools.NewRegistry()
			reg.Register(tools.TextResponse{})
			toolName := mountForms(t, reg, formServer(t, mode.classic))
			client := agenttest.NewFakeClient(
				agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", toolName, "{}")),
				agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
			)
			r, conv := newRealFormRunner(t, client, reg, 2)
			convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
			if err != nil {
				t.Fatalf("NewConversationWithID: %v", err)
			}
			_, srv := newDetachTestServer(t, r, conv, ServerConfig{})

			resp := postRun(t, srv, steerRunPayload(convID))
			defer resp.Body.Close()
			frames := sseData(resp.Body)
			q := awaitQuestion(t, frames)
			if q.Server != "forms" || q.Tool != "ask_name" || len(q.Fields) != 1 || q.Fields[0].Name != "name" {
				t.Fatalf("question = %+v", q)
			}

			time.Sleep(3 * time.Second)
			if status, body := answerPost(t, srv, q.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
				t.Fatalf("answer = %d %s, want 202", status, body)
			}
			rest := drainFrames(t, frames)
			if !strings.Contains(rest, `"type":"RUN_FINISHED"`) || !strings.Contains(rest, ElicitationResolvedEventName) {
				t.Fatalf("the run did not finish with the form resolved:\n%s", rest)
			}
			reqs := client.RecordedRequests()
			if len(reqs) < 2 || !strings.Contains(joinMessageContents(reqs[1].Messages), "hello Ada") {
				t.Fatalf("the model never saw the server's greeting: %+v", reqs)
			}

			replay := readFullBody(t, mustGet(t, srv.URL+"/agent/runs/"+q.RunID+"/events"))
			if strings.Count(replay, `"name":"`+ElicitationEventName+`"`) != 1 || strings.Count(replay, `"name":"`+ElicitationResolvedEventName+`"`) != 1 {
				t.Fatalf("a replay must carry the question and its resolution once each:\n%s", replay)
			}
		})
	}
}

// sleepyTool waits 3 s holding no clock: the control that shows the harness's 2 s
// wallclock is real, without which the held test above would prove nothing.
type sleepyTool struct{}

func (sleepyTool) Spec() tools.Spec {
	return tools.Spec{Name: "sleepy", Summary: "waits three seconds", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (sleepyTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	select {
	case <-ctx.Done():
		return tools.ToolResult{}, ctx.Err()
	case <-time.After(3 * time.Second):
		return tools.ToolResult{Preview: "slept"}, nil
	}
}

func TestTheShortWallclockCutsAnUnheldTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(sleepyTool{})
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "sleepy", "{}")),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
	)
	r, conv := newRealFormRunner(t, client, reg, 2)
	convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("NewConversationWithID: %v", err)
	}
	_, srv := newDetachTestServer(t, r, conv, ServerConfig{})

	readFullBody(t, postRun(t, srv, steerRunPayload(convID)))
	for i, req := range client.RecordedRequests() {
		if strings.Contains(joinMessageContents(req.Messages), "slept") {
			t.Fatalf("request %d carries the tool's result: the 2 s wallclock never cut the 3 s tool", i)
		}
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}
