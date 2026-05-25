package agentdef

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	registry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
)

type recordingDelegateRunner struct {
	calls     int
	archetype string
	prompt    string
	model     string
	chain     []string
}

func (r *recordingDelegateRunner) Run(_ context.Context, archetype, prompt, modelOverride string, parentChain []string) (string, error) {
	r.calls++
	r.archetype = archetype
	r.prompt = prompt
	r.model = modelOverride
	r.chain = append([]string(nil), parentChain...)
	return "delegated result", nil
}

type allowDelegateAuth struct{}

func (allowDelegateAuth) Authorize(context.Context, identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return identity.AuthorizationDecision{Decision: identity.DecisionAllow}, nil
}

type denyDelegateAuth struct{}

func (denyDelegateAuth) Authorize(context.Context, identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return identity.AuthorizationDecision{Decision: identity.DecisionDeny, Reason: "test"}, nil
}

func TestWithArchetypeDelegates_DefaultNameAndPrefix(t *testing.T) {
	runner := &recordingDelegateRunner{}
	tools := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), runner, []string{DefaultChatArchetype}, []string{"search"}, nil)
	if len(tools) != 1 {
		t.Fatalf("delegate tools = %d, want 1", len(tools))
	}
	if tools[0].Name() != "delegate_summarizer" {
		t.Fatalf("name = %q", tools[0].Name())
	}
	if got := tools[0].Description(); !strings.HasPrefix(got, overDelegationPrefix) {
		t.Fatalf("description missing over-delegation prefix: %q", got)
	}
}

func TestWithArchetypeDelegates_CustomDelegateName(t *testing.T) {
	reg := testDelegateRegistry([]SubagentRef{{ID: "summarizer", DelegateName: "delegate_payload"}})
	tools := WithArchetypeDelegates(DefaultChatArchetype, reg, &recordingDelegateRunner{}, []string{DefaultChatArchetype}, nil, nil)
	if len(tools) != 1 || tools[0].Name() != "delegate_payload" {
		t.Fatalf("delegate tools = %+v, want custom delegate_payload", toolNames(tools))
	}
}

func TestWithArchetypeDelegates_CollisionSkipsSynth(t *testing.T) {
	tools := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), &recordingDelegateRunner{}, []string{DefaultChatArchetype}, []string{"delegate_summarizer"}, nil)
	if len(tools) != 0 {
		t.Fatalf("delegate collision produced tools: %+v", toolNames(tools))
	}
}

func TestWithArchetypeDelegates_SkipsRecursiveChain(t *testing.T) {
	tools := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), &recordingDelegateRunner{}, []string{DefaultChatArchetype, "summarizer"}, nil, nil)
	if len(tools) != 0 {
		t.Fatalf("recursive delegate produced tools: %+v", toolNames(tools))
	}
}

func TestDelegateToolExecuteCallsRunnerWithArchetype(t *testing.T) {
	runner := &recordingDelegateRunner{}
	tools := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), runner, []string{DefaultChatArchetype}, nil, nil)
	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), allowDelegateAuth{}), "actor-1")
	out, err := tools[0].Execute(ctx, map[string]any{"prompt": "summarize this", "model": "fast"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "delegated result" {
		t.Fatalf("Execute result = %q", out)
	}
	if runner.calls != 1 || runner.archetype != "summarizer" || runner.prompt != "summarize this" || runner.model != "fast" {
		t.Fatalf("runner call = %+v", runner)
	}
	if want := []string{DefaultChatArchetype, "summarizer"}; !reflect.DeepEqual(runner.chain, want) {
		t.Fatalf("runner chain = %+v, want %+v", runner.chain, want)
	}
}

func TestDelegateToolRequiresPrompt(t *testing.T) {
	runner := &recordingDelegateRunner{}
	tools := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), runner, []string{DefaultChatArchetype}, nil, nil)
	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), allowDelegateAuth{}), "actor-1")
	_, err := tools[0].Execute(ctx, map[string]any{"prompt": "   "})
	if err == nil || runner.calls != 0 {
		t.Fatalf("Execute err = %v, runner calls = %d; want prompt rejection before runner", err, runner.calls)
	}
}

func TestDelegateToolRequiresSwarmSpawnAuthorization(t *testing.T) {
	tools := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), &recordingDelegateRunner{}, []string{DefaultChatArchetype}, nil, nil)
	_, err := tools[0].Execute(identity.WithAuthorizer(context.Background(), denyDelegateAuth{}), map[string]any{"prompt": "go"})
	if err == nil || !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("Execute err = %v, want unauthorized", err)
	}
}

func testDelegateRegistry(subagents []SubagentRef) *Registry {
	if subagents == nil {
		subagents = []SubagentRef{{ID: "summarizer"}}
	}
	reg := &Registry{
		defs: map[string]AgentDefinition{
			DefaultChatArchetype: {
				ID:        DefaultChatArchetype,
				Tier:      TierChat,
				WhenToUse: "front-line chat",
				Prompt:    "chat",
				Subagents: append([]SubagentRef(nil), subagents...),
			},
			"summarizer": {
				ID:        "summarizer",
				Tier:      TierWorker,
				WhenToUse: "Summarizes large payloads.",
				Prompt:    "summarize",
			},
		},
		ids: []string{DefaultChatArchetype, "summarizer"},
	}
	return reg
}

func toolNames(tools []registry.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name())
	}
	return out
}
