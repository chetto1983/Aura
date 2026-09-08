package swarm

import "testing"

func TestDelegationWorkerIDsSeparateMeasuredShortHashCollision(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	const conv = "22222222-2222-2222-2222-222222222222"
	first := delegationIdempotencyKey(owner, conv, "agent_tool:probe:62646", 0, "independent task")
	second := delegationIdempotencyKey(owner, conv, "agent_tool:probe:102602", 0, "independent task")
	if first == second {
		t.Fatal("test requires different durable operations")
	}
	if delegationChildID(first, 0) == delegationChildID(second, 0) {
		t.Fatal("different operations share transcript and control identity w1-c4ea2263")
	}
}
