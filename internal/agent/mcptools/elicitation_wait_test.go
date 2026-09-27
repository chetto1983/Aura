package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
)

// Review Focus 1: the operator's 600 ms must not count against a 200 ms call bound.
func TestAHeldCallOutlivesItsTimeout(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.2")
	tool := bridgedFormTool(t)
	asker := newFakeAsker(acceptName("Ada"), 600*time.Millisecond)

	res, err := tool.Execute(runCtx(t, asker), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v; the call's clock must stop while the operator answers", err)
	}
	if !strings.Contains(res.Preview, "hello Ada") {
		t.Fatalf("preview = %q, want the server's greeting", res.Preview)
	}
}

// Review Focus 2: the call's 200 ms bound is earlier than the question's 1 s one,
// and the question must still expire on its own clock. An expiry answers cancel:
// nobody made an explicit choice.
func TestAnUnansweredQuestionExpiresWhileTheCallIsHeld(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.2")
	t.Setenv(envMCPElicitationTimeoutSec, "1")
	tool := bridgedFormTool(t)
	asker := newFakeAsker(acceptName("Ada"), never)

	start := time.Now()
	res, err := tool.Execute(runCtx(t, asker), json.RawMessage(`{}`))
	elapsed := time.Since(start)
	if err != nil || !strings.Contains(res.Preview, "cancel") {
		t.Fatalf("Execute = %q, %v; an expired question cancels, it does not fail the call", res.Preview, err)
	}
	if cause := asker.endedWith(t); !errors.Is(cause, elicit.ErrExpired) {
		t.Fatalf("the wait ended with %v, want elicit.ErrExpired", cause)
	}
	if elapsed < time.Second || elapsed > 5*time.Second {
		t.Fatalf("the question closed after %v, want its own 1s bound", elapsed)
	}
}

func TestTheWaitCancelsWhenTheCallEnds(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), never)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})
	ctx, cancel := context.WithCancel(elicit.WithAsker(context.Background(), asker))
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(ctx, "ask_name", nil)
		done <- err
	}()
	asker.question(t)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled call returned no error")
	}
	if cause := asker.endedWith(t); cause == nil || errors.Is(cause, elicit.ErrExpired) {
		t.Fatalf("the wait ended with %v, want the call's own end, not an expiry", cause)
	}
}

func TestAClassicWaitEndsOnlyWhenEveryCallHasEnded(t *testing.T) {
	first, endFirst := context.WithCancel(context.Background())
	second, endSecond := context.WithCancel(context.Background())
	wait, stop := waitContext(context.Background(), []context.Context{first, second})
	defer stop()

	endFirst()
	select {
	case <-wait.Done():
		t.Fatal("the wait ended with a call still open")
	case <-time.After(50 * time.Millisecond):
	}
	endSecond()
	select {
	case <-wait.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the wait outlived every call")
	}
	if !errors.Is(context.Cause(wait), context.Canceled) {
		t.Fatalf("cause = %v, want the calls' own", context.Cause(wait))
	}
}

func TestAClassicQuestionEndsWhenItsToolCallReturns(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), never)
	entered, release := make(chan struct{}), make(chan struct{})
	srv, serverSession := elicitingMount(t, classicOnly, declining(), map[string]sdkmcp.ToolHandler{
		"hold": holdingTool(entered, release),
	})
	ctx, cancel := context.WithCancel(elicit.WithAsker(context.Background(), asker))
	defer cancel()
	called := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(ctx, "hold", nil)
		called <- err
	}()
	<-entered
	type reply struct {
		result *sdkmcp.ElicitResult
		err    error
	}
	answered := make(chan reply, 1)
	go func() {
		result, err := serverSession.Elicit(ctx, &sdkmcp.ElicitParams{Message: "name?", RequestedSchema: nameForm()})
		answered <- reply{result, err}
	}()
	asker.question(t)
	close(release)
	if err := <-called; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-answered:
		if got.err != nil || got.result.Action != elicit.ActionCancel {
			t.Fatalf("Elicit = %+v, %v; a finished call must cancel its question", got.result, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("the question outlived its finished tool call")
	}
}
