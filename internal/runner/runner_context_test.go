package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// deadlineClient records how long the context it was called with had left.
type deadlineClient struct {
	*agenttest.FakeClient
	left time.Duration
}

func (c *deadlineClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if deadline, ok := ctx.Deadline(); ok {
		c.left = time.Until(deadline)
	}
	return c.FakeClient.Stream(ctx, req)
}

func summarizerRuntime(client llm.Client) llm.RuntimeSnapshot {
	return llm.RuntimeSnapshot{Client: client, Config: llm.Config{Model: "route-model", ContextWindow: 32768, TotalTimeoutSec: 120}}
}

func TestCompactionSummarizerUsesTheRouteUnlessItHasAModel(t *testing.T) {
	rounds := []llm.Message{{Role: llm.RoleUser, Content: "ciao"}, {Role: llm.RoleAssistant, Content: "ciao!"}}
	for name, tc := range map[string]struct{ model, want string }{
		"no model of its own": {"", "route-model"},
		"a model of its own":  {"summary-model", "summary-model"},
	} {
		t.Run(name, func(t *testing.T) {
			client := &deadlineClient{FakeClient: agenttest.NewFakeClient(agenttest.TextChunks("stop", "the summary"))}
			s := (&Runner{compactionEnabled: true, compactionModel: tc.model}).compactionSummarizer(summarizerRuntime(client))
			got, err := s.Summarize(context.Background(), rounds)
			if err != nil || got != "the summary" {
				t.Fatalf("Summarize = %q, %v", got, err)
			}
			if model := client.LastRequest().Model; model != tc.want {
				t.Fatalf("model = %q, want %q", model, tc.want)
			}
			if client.left <= 110*time.Second || client.left > 120*time.Second {
				t.Fatalf("summary call had %v left, want the route's 120 s total timeout", client.left)
			}
		})
	}
}

func TestCompactionSummarizerIsOffWithCompaction(t *testing.T) {
	if s := (&Runner{}).compactionSummarizer(summarizerRuntime(agenttest.NewFakeClient())); s != nil {
		t.Fatalf("summarizer = %T, want none while compaction is off", s)
	}
}

type profileFake struct{ block string }

func (profileFake) Timezone(context.Context, string) string       { return "" }
func (p profileFake) ProfileBlock(context.Context, string) string { return p.block }

func TestRenderContextBlockJoinsTheAlwaysBlockAndTheProfile(t *testing.T) {
	r := &Runner{
		alwaysBlock: func(context.Context) string { return " always-on skills \n" },
		profiles:    profileFake{block: "\nname: Ada\n"},
	}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	if got, want := r.renderContextBlock(ctx), "always-on skills\n\nname: Ada"; got != want {
		t.Fatalf("context block = %q, want %q", got, want)
	}
	if got := r.renderContextBlock(context.Background()); got != "always-on skills" {
		t.Fatalf("without an identity the block = %q, want the skills alone", got)
	}
}
