package mcptools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMRTRElicitationAsksTheCallsRun(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; want the operator's answer back from the server", got, err)
	}
	q := asker.question(t)
	if q.Server != "fixture" || q.Tool != "ask_name" || q.Message != "what is your name" {
		t.Fatalf("question = %+v", q)
	}
	if len(q.Fields) != 1 || q.Fields[0].Name != "name" || !q.Fields[0].Required || q.Fields[0].Kind != elicit.KindString {
		t.Fatalf("fields = %+v", q.Fields)
	}
	if time.Until(q.Deadline) <= 0 {
		t.Fatalf("deadline %v is not ahead", q.Deadline)
	}
	session, err := srv.currentSession()
	if err != nil {
		t.Fatal(err)
	}
	if open := inFlight.on(session); len(open) != 0 {
		t.Fatalf("%d calls still recorded open after they returned", len(open))
	}
}

func TestClassicElicitationAsksTheOneRunInFlight(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, classicOnly, declining(), map[string]sdkmcp.ToolHandler{"ask_name": classicAsk})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; a classic request with one run in flight goes to that run", got, err)
	}
	if q := asker.question(t); q.Tool != "ask_name" {
		t.Fatalf("tool = %q, want the open call's", q.Tool)
	}
}

// A run with no cockpit keeps today's decline-and-surface, told on the identity of
// the call that made the request.
func TestAFormFromARunWithNoCockpitReachesItsOperatorsChannel(t *testing.T) {
	for name, fixture := range map[string]struct {
		opts    *sdkmcp.ServerOptions
		handler sdkmcp.ToolHandler
	}{
		"mrtr":    {nil, mrtrAsk("what is your name", nameForm())},
		"classic": {classicOnly, classicAsk},
	} {
		t.Run(name, func(t *testing.T) {
			consent := declining()
			srv, _ := elicitingMount(t, fixture.opts, consent, map[string]sdkmcp.ToolHandler{"ask_name": fixture.handler})

			ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
			got, err := srv.CallToolText(ctx, "ask_name", nil)
			if err != nil || got != "decline" {
				t.Fatalf("CallToolText = %q, %v, want the fallback's decline", got, err)
			}
			told := consent.next(t)
			if told.identity != "identity-a" || told.q.Message != "what is your name" || told.q.Refusal != "" {
				t.Fatalf("the fallback was told %+v", told)
			}
		})
	}
}

// Review Focus 3.
func TestClassicElicitationWithTwoRunsInFlightAsksNeither(t *testing.T) {
	consent := newRecordingConsent(elicit.ActionAccept)
	runA, runB := newFakeAsker(acceptName("Ada"), 0), newFakeAsker(acceptName("Bob"), 0)

	got := sharedSession(t, consent,
		elicit.WithAsker(context.Background(), runA), elicit.WithAsker(context.Background(), runB))
	if got != "decline" {
		t.Fatalf("CallToolText = %q; with two runs on the session the server must be declined", got)
	}
	assertBareRefusal(t, "run A", runA.question(t))
	assertBareRefusal(t, "run B", runB.question(t))
	consent.none(t)
}

func TestClassicElicitationSharedWithAChannelRunTellsItsOperatorToo(t *testing.T) {
	consent := newRecordingConsent(elicit.ActionAccept)
	cockpit := newFakeAsker(acceptName("Ada"), 0)

	got := sharedSession(t, consent,
		identityctx.WithIdentityID(context.Background(), "identity-b"), elicit.WithAsker(context.Background(), cockpit))
	if got != "decline" {
		t.Fatalf("CallToolText = %q, want decline", got)
	}
	assertBareRefusal(t, "the cockpit run", cockpit.question(t))
	told := consent.next(t)
	if told.identity != "identity-b" {
		t.Fatalf("the channel run's notice went to %q", told.identity)
	}
	assertBareRefusal(t, "the channel run", told.q)
}

// Two runs with no cockpit share a nil asker, and the identity is what tells them
// apart.
func TestClassicElicitationWithTwoOperatorsOnOneSessionTellsEachWithoutTheForm(t *testing.T) {
	consent := newRecordingConsent(elicit.ActionAccept)
	got := sharedSession(t, consent,
		identityctx.WithIdentityID(context.Background(), "identity-a"),
		identityctx.WithIdentityID(context.Background(), "identity-b"))
	if got != "decline" {
		t.Fatalf("CallToolText = %q; two operators' runs on one session must decline", got)
	}
	told := map[string]bool{}
	for range 2 {
		got := consent.next(t)
		assertBareRefusal(t, got.identity, got.q)
		told[got.identity] = true
	}
	if !told["identity-a"] || !told["identity-b"] {
		t.Fatalf("told %v, want each operator once", told)
	}
}

func TestClassicElicitationOutsideAnyCallFallsBack(t *testing.T) {
	consent := declining()
	_, serverSession := elicitingMount(t, classicOnly, consent, map[string]sdkmcp.ToolHandler{"ask_name": classicAsk})

	res, err := serverSession.Elicit(context.Background(), &sdkmcp.ElicitParams{Mode: "form", Message: "anyone there?", RequestedSchema: nameForm()})
	if err != nil || res.Action != elicit.ActionDecline {
		t.Fatalf("Elicit = %+v, %v; want the fallback's decline", res, err)
	}
	if told := consent.next(t); told.q.Message != "anyone there?" || told.q.Server != "fixture" || told.q.Refusal != "" {
		t.Fatalf("fallback saw %+v", told.q)
	}
}

// A form a server asks while its call's result links are read back belongs to the
// same run: the SDK runs it on the resources/read context (mrtr.go:76).
func TestAFormAskedWhileReadingLinksAsksTheSameRun(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(mustTool("lookup", "Links a resource.", nil, nil), func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.ResourceLink{URI: "fixture://card", Name: "card"}}}, nil
	})
	server.AddResource(&sdkmcp.Resource{URI: "fixture://card", Name: "card"}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: "fixture://card", Text: fmt.Sprintf("card for %v", reply.Content["name"])}}}, nil
		}
		return &sdkmcp.ReadResourceResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: "whose card?", RequestedSchema: nameForm()},
		}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	o := mcpSessionOptionsFor(srv)
	o.Elicitation = NewElicitationHandler("fixture", declining())
	session, err := connectClient(context.Background(), clientTransport, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	asker := newFakeAsker(acceptName("Ada"), 0)
	if _, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "lookup", nil); err != nil {
		t.Fatalf("CallToolText: %v", err)
	}
	if q := asker.question(t); q.Message != "whose card?" || q.Tool != "lookup" {
		t.Fatalf("the links' form reached the run as %+v", q)
	}
}

// An identity-scoped mount recurses into its child's CallTool, so its calls are
// open on the child's session.
func TestAClassicFormThroughAnIdentityScopedMountAsksTheRun(t *testing.T) {
	handler := NewElicitationHandler("remote", declining())
	connect := func(_ context.Context, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, classicOnly)
		server.AddTool(mustTool("ask_name", "Elicits.", nil, nil), classicAsk)
		clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(context.Background(), serverTransport, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = serverSession.Close() })
		o.Elicitation = handler
		return connectClient(hctx, clientTransport, o)
	}
	parent := NewMountedServer("remote", nil)
	parent.identityPool = newIdentitySessionPool(parent, connect, t.Context())
	t.Cleanup(func() { _ = parent.Close() })
	identity := identityctx.WithIdentityID(t.Context(), "identity-a")
	_, advertised, err := parent.identityPool.openInitial(identity)
	if err != nil {
		t.Fatalf("open initial: %v", err)
	}
	parent.trackAcceptedTools(advertised)

	asker := newFakeAsker(acceptName("Ada"), 0)
	got, err := parent.CallToolText(elicit.WithAsker(identity, asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; the call open on the child's session must place the form", got, err)
	}
}

// The read-only redial reissues the call at
// bridge_supervisor.go:339, and that attempt must be marked too.
func TestAReissuedReadOnlyCallStillRoutesItsForm(t *testing.T) {
	readOnly := &sdkmcp.ToolAnnotations{ReadOnlyHint: true}
	fixture := &scriptedOpen{t: t, build: func() *sdkmcp.Server {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
		server.AddTool(mustTool("ask_name", "Elicits.", nil, readOnly), mrtrAsk("what is your name", nameForm()))
		return server
	}}
	handler := NewElicitationHandler("fixture", declining())
	srv := NewMountedServer("fixture", func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Elicitation = handler
		return fixture.open(pctx, hctx, o)
	})
	first, err := fixture.open(context.Background(), context.Background(), mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatal(err)
	}
	srv.Attach(first)
	t.Cleanup(func() { _ = srv.Close() })
	srv.trackBridgedTools(bridgeTools("fixture", srv, []*sdkmcp.Tool{mustTool("ask_name", "Elicits.", nil, readOnly)}, time.Second))
	killLiveSession(t, srv)

	asker := newFakeAsker(acceptName("Ada"), 0)
	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; the reissued call's form must reach its run", got, err)
	}
}

// One server cannot open more than elicit.MaxOpenQuestions
// cards at once.
func TestAFloodOfFormsOpensNoMoreThanTheCap(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), time.Second)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"flood": floodAsk(elicit.MaxOpenQuestions + 2)})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "flood", nil)
	if err != nil || got != "declined 2" {
		t.Fatalf("CallToolText = %q, %v; the requests over the cap must be declined", got, err)
	}
	if n := len(asker.asked); n != elicit.MaxOpenQuestions {
		t.Fatalf("the run was asked %d questions at once, want the cap of %d", n, elicit.MaxOpenQuestions)
	}
}

func TestAnOverCapFormIsShownAsARefusal(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{
		"ask_name": mrtrAsk(strings.Repeat("m", elicit.MaxMessageBytes+1), nameForm()),
	})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v, want decline", got, err)
	}
	if q := asker.question(t); q.Refusal != elicit.RefusalUnrenderable || q.Message != "" || q.Fields != nil {
		t.Fatalf("question = %+v; an over-cap form is shown as a refusal carrying none of the server's text", q)
	}
}

func TestURLModeIsRefusedBeforeTheRunIsAsked(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	ctx := withCallTool(elicit.WithAsker(context.Background(), asker), "login")
	res, err := NewElicitationHandler("fixture", newRecordingConsent(elicit.ActionAccept))(ctx, &sdkmcp.ElicitRequest{
		Params: &sdkmcp.ElicitParams{Mode: "url", URL: "https://evil.example/phish", ElicitationID: "e1"},
	})
	if err != nil || res.Action != elicit.ActionDecline {
		t.Fatalf("handler = %+v, %v; want decline", res, err)
	}
	select {
	case q := <-asker.asked:
		t.Fatalf("the run was asked a url-mode question: %+v", q)
	default:
	}
}

func TestAPanickingAskerDeclines(t *testing.T) {
	logs := captureLogs(t, slog.LevelWarn)
	asker := newFakeAsker(acceptName("Ada"), 0)
	asker.panics = true
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v, want decline", got, err)
	}
	if !strings.Contains(logs.String(), `reason="the run's asker failed"`) {
		t.Fatalf("the panic was not recorded as the asker failing:\n%s", logs.String())
	}
}

// The spec's rule: the action, the server and the field count are logged; what the
// operator typed never is.
func TestTheResolvedLogLineCarriesNoAnswerValue(t *testing.T) {
	logs := captureLogs(t, slog.LevelInfo)
	asker := newFakeAsker(acceptName("Ada-7f3a-secret"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	if _, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil); err != nil {
		t.Fatal(err)
	}
	line := logs.String()
	for _, want := range []string{"mcp elicitation resolved", "action=accept", "fields=1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in:\n%s", want, line)
		}
	}
	if strings.Contains(line, "Ada-7f3a-secret") {
		t.Fatalf("the operator's answer reached the log:\n%s", line)
	}
}
