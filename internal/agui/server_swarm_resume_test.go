package agui

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
)

func TestSwarmTranscriptReplayIncludesResumedAttempt(t *testing.T) {
	body := append(swarmTextLine(t, "before pause"), swarmEventLine(t, agent.Event{Actions: agent.Actions{
		AwaitingInput: &agent.AwaitingInput{Question: "Choose 7 or 9", ToolCallID: "ask-worker"},
	}})...)
	body = append(body, swarmTerminalLine(t, "needs_user_input")...)
	body = append(body, swarmTextLine(t, "resumed calculation result: 99")...)
	body = append(body, swarmTerminalLine(t, "ok")...)
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": body})
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)
	resp, err := http.Get(srv.URL + swarmWorkerEventsPath(goodID, "w1"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "resumed calculation result: 99") {
		t.Fatalf("worker replay stopped at an earlier pause: %s", data)
	}
}
