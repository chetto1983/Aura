package mcptools

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
)

// fakeAsker stands in for a run's cockpit: it records every question, takes
// after to answer, and keeps the Asker contract when ctx ends first.
type fakeAsker struct {
	answer elicit.Answer
	after  time.Duration
	panics bool
	asked  chan elicit.Question
	ended  chan error
}

const never = time.Hour

func newFakeAsker(answer elicit.Answer, after time.Duration) *fakeAsker {
	return &fakeAsker{answer: answer, after: after, asked: make(chan elicit.Question, 8), ended: make(chan error, 8)}
}

func acceptName(name string) elicit.Answer {
	return elicit.Answer{Action: elicit.ActionAccept, Content: map[string]any{"name": name}}
}

func (f *fakeAsker) Ask(ctx context.Context, q elicit.Question) (elicit.Answer, error) {
	f.asked <- q
	if f.panics {
		panic("the cockpit blew up")
	}
	if q.Refusal != "" {
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	timer := time.NewTimer(f.after)
	defer timer.Stop()
	select {
	case <-timer.C:
		return f.answer, nil
	case <-ctx.Done():
		f.ended <- context.Cause(ctx)
		return elicit.Answer{}, context.Cause(ctx)
	}
}

func (f *fakeAsker) question(t *testing.T) elicit.Question {
	t.Helper()
	select {
	case q := <-f.asked:
		return q
	case <-time.After(5 * time.Second):
		t.Fatal("the run's asker was never asked")
		return elicit.Question{}
	}
}

func (f *fakeAsker) endedWith(t *testing.T) error {
	t.Helper()
	select {
	case cause := <-f.ended:
		return cause
	case <-time.After(5 * time.Second):
		t.Fatal("the asker's wait never ended")
		return nil
	}
}

// recordingConsent is the fallback as a test sees it: every question it was told,
// with the identity of the context it was told on.
type recordingConsent struct {
	action string
	told   chan toldQuestion
}

type toldQuestion struct {
	identity string
	q        elicit.Question
}

func newRecordingConsent(action string) *recordingConsent {
	return &recordingConsent{action: action, told: make(chan toldQuestion, 8)}
}

func (c *recordingConsent) AskOperator(ctx context.Context, q elicit.Question) (string, map[string]any, error) {
	c.told <- toldQuestion{identity: identityctx.IdentityID(ctx), q: q}
	return c.action, nil, nil
}

func (c *recordingConsent) next(t *testing.T) toldQuestion {
	t.Helper()
	select {
	case got := <-c.told:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the fallback was never told")
		return toldQuestion{}
	}
}

// none checks the fallback stays silent. The refusal notices travel on their own
// goroutines, so it waits a moment before believing it.
func (c *recordingConsent) none(t *testing.T) {
	t.Helper()
	select {
	case got := <-c.told:
		t.Fatalf("the fallback was told %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func nameForm() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string", "title": "Name"}},
		"required":   []any{"name"},
	}
}

func textResult(format string, args ...any) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

// greeting is what every fixture answers once it has a reply: the action, or
// the name when the operator accepted.
func greeting(reply *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	if reply.Action != elicit.ActionAccept {
		return textResult("%s", reply.Action)
	}
	return textResult("hello %v", reply.Content["name"])
}

// mrtrAsk elicits the way a 2026-07-28 server must: it returns InputRequests, and
// the client calls the tool again with the reply (go-sdk@v1.8.0 mcp/mrtr.go).
func mrtrAsk(message string, form map[string]any) sdkmcp.ToolHandler {
	return func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return greeting(reply), nil
		}
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: message, RequestedSchema: form},
		}}, nil
	}
}

// floodAsk asks n forms in one round, the way an errant server floods a client,
// and reports how many it saw declined.
func floodAsk(n int) sdkmcp.ToolHandler {
	return func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if len(req.Params.InputResponses) > 0 {
			declined := 0
			for _, reply := range req.Params.InputResponses {
				if r, ok := reply.(*sdkmcp.ElicitResult); ok && r.Action == elicit.ActionDecline {
					declined++
				}
			}
			return textResult("declined %d", declined), nil
		}
		asks := sdkmcp.InputRequestMap{}
		for i := range n {
			asks[fmt.Sprintf("q%d", i)] = &sdkmcp.ElicitParams{Mode: "form", Message: "again", RequestedSchema: nameForm()}
		}
		return &sdkmcp.CallToolResult{InputRequests: asks}, nil
	}
}

// classicAsk elicits with a server-to-client request made while its call is open.
func classicAsk(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	reply, err := req.Session.Elicit(ctx, &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: nameForm()})
	if err != nil {
		return nil, err
	}
	return greeting(reply), nil
}

// classicOnly narrows a fixture to the last protocol that allows a classic
// elicitation/create (go-sdk@v1.8.0 mcp/server.go:1619-1627).
var classicOnly = &sdkmcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}}

// elicitingMount serves handlers from an in-memory fixture, mounted with the
// elicitation handler production installs.
func elicitingMount(t *testing.T, opts *sdkmcp.ServerOptions, consent ElicitationConsent, handlers map[string]sdkmcp.ToolHandler) (*MountedServer, *sdkmcp.ServerSession) {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, opts)
	for name, handler := range handlers {
		server.AddTool(mustTool(name, "Elicits.", nil, nil), handler)
	}
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	o := mcpSessionOptionsFor(srv)
	o.Elicitation = NewElicitationHandler("fixture", consent)
	session, err := connectClient(ctx, clientTransport, o)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)
	return srv, serverSession
}

func declining() *recordingConsent { return newRecordingConsent(elicit.ActionDecline) }

// holdingTool keeps its call open until release closes, so a second run can
// share the session.
func holdingTool(entered, release chan struct{}) sdkmcp.ToolHandler {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return textResult("held"), nil
	}
}

// sharedSession runs a call that stays open on the fixture's session under first,
// then asks for a name under second, and returns what the second call got.
func sharedSession(t *testing.T, consent ElicitationConsent, first, second context.Context) string {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	srv, _ := elicitingMount(t, classicOnly, consent, map[string]sdkmcp.ToolHandler{
		"hold": holdingTool(entered, release), "ask_name": classicAsk,
	})
	held := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(first, "hold", nil)
		held <- err
	}()
	<-entered
	got, err := srv.CallToolText(second, "ask_name", nil)
	close(release)
	if heldErr := <-held; heldErr != nil {
		t.Fatalf("the held call failed: %v", heldErr)
	}
	if err != nil {
		t.Fatalf("CallToolText: %v", err)
	}
	return got
}

// assertBareRefusal checks an ambiguous-run notice carries none of the server's
// text: the form may belong to another conversation, even another operator's.
func assertBareRefusal(t *testing.T, who string, q elicit.Question) {
	t.Helper()
	if q.Refusal != elicit.RefusalAmbiguousRun || q.Message != "" || q.Fields != nil || q.Server != "fixture" {
		t.Fatalf("%s was shown %+v, want the ambiguous-run refusal with none of the server's text", who, q)
	}
}

// bridgedFormTool mounts one MRTR form tool and bridges it the way a registry
// sees it, so Execute runs the real call bound (bridge_call.go).
func bridgedFormTool(t *testing.T) tools.Tool {
	t.Helper()
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})
	bridged, err := bridgeDefault(context.Background(), "forms", srv)
	if err != nil || len(bridged) != 1 {
		t.Fatalf("bridgeDefault = %d tools, %v", len(bridged), err)
	}
	return bridged[0]
}

// runCtx is a run's tool-call context, bounded at 5 s so a regression fails the
// test instead of hanging it on a never-answering asker.
func runCtx(t *testing.T, asker elicit.Asker) context.Context {
	ctx, cancel := context.WithTimeout(tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048), 5*time.Second)
	t.Cleanup(cancel)
	return elicit.WithAsker(ctx, asker)
}
