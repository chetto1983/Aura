package runner

import (
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// agentRequests returns only the recorded requests that belong to the agent loop:
// the ones carrying the turn's tool manifest.
//
// The fake client sees more than the agent's rounds. maybeAutoTitle
// (runner_resume.go) fires once a conversation passes autoTitleMinSeq turns, it
// deliberately outlives the turn ctx (context.WithoutCancel), and it calls
// GenerateTitle on the SAME client. So an extra request is always issued; the only
// question is whether it lands before a test reads the slice. Under CPU contention
// it does, and any assertion counting or indexing raw RecordedRequests is then
// wrong by one -- intermittently, and never when the test is run alone.
//
// Measured, not theorised: CI run 34093230797 failed with
// "runner_steer_leftover_test.go:361: LLM calls = 4, want 3", with
// "WARN runner: auto-title generation failed" logged immediately above it, and the
// sibling assertion at line 110 reproduced the same off-by-one locally under a
// full -race matrix. Both pass in isolation, which is how it survived.
//
// The filter is the discriminator conversations.GenerateTitle itself guarantees: it
// sets ToolChoice "none" and sends no Tools, so a title call can never be mistaken
// for a round. This lived in the db_integration tier as agentRounds; it is here now
// because the unit tier needs the same insight and one copy is enough.
func agentRequests(client *agenttest.FakeClient) []llm.Request {
	var rounds []llm.Request
	for _, req := range client.RecordedRequests() {
		if len(req.Tools) > 0 {
			rounds = append(rounds, req)
		}
	}
	return rounds
}

// agentRounds counts what agentRequests returns.
func agentRounds(client *agenttest.FakeClient) int { return len(agentRequests(client)) }
