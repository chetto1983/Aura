package agui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
)

func TestWorkerStatusReportsPersistenceWithoutTranscriptContent(t *testing.T) {
	state := &swarmWorkerStatusState{}
	state.ingest("w1", swarmTerminalLine(t, "ok"), time.Now())
	if state.payload("w1", time.Now(), 0).Reported {
		t.Fatal("model completion is not report persistence")
	}
	state.ingest("w1", swarmEventLine(t, agent.Event{Actions: agent.Actions{StateDelta: map[string]any{
		"swarm_child_status": "ok", "swarm_report_recorded": true,
	}}}), time.Now())
	payload := state.payload("w1", time.Now(), 0)
	if !payload.Reported {
		t.Fatal("durable report notification missing")
	}
	data, err := json.Marshal(payload)
	if err != nil || strings.Contains(string(data), "summary") {
		t.Fatalf("unexpected status payload: %s %v", data, err)
	}
}
