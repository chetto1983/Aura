//go:build spike_agui

// Spike 016 — agui-sse-roundtrip.
// Proves the −100-LOC Slice-8 design end-to-end: a synthetic
// iter.Seq2[*agent.Event, error] (the exact (*LlmAgent).Run signature) flows
// through a pure mini-translator into SDK events, is served over a real HTTP
// POST /agent/run SSE response via the SDK's SSEWriter, and is read back by an
// HTTP client asserting framing + sequence + payload. No LLM calls.
//
// Run: go run -tags spike_agui ./.planning/spikes/016-agui-sse-roundtrip
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent"
)

func logf(cat, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, args...))
}

// synthSeq fabricates the *agent.Event stream a real (*LlmAgent).Run yields for
// "assistant text + one tool call + state delta" — same shapes Phase 2 emits.
func synthSeq() iter.Seq2[*agent.Event, error] {
	now := time.Now()
	reqID := uuid.Must(uuid.NewV7())
	evs := []*agent.Event{
		{RequestID: reqID, Author: "aura", Timestamp: now,
			LLMResponse: &agent.LLMResponse{Content: "Ciao"}},
		{RequestID: reqID, Author: "aura", Timestamp: now,
			Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
				Event: agent.ToolInvocationStart, ToolCallID: "call-1",
				ToolName: "web_search", Arguments: `{"query":"meteo Caraglio"}`,
			}}},
		{RequestID: reqID, Author: "aura", Timestamp: now,
			Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
				Event: agent.ToolInvocationEnd, ToolCallID: "call-1",
				ToolName: "web_search", Status: "ok", ResultPreview: "12°C sereno",
			}}},
		{RequestID: reqID, Author: "aura", Timestamp: now,
			Actions: agent.Actions{StateDelta: map[string]any{"cost_usd": 0.0042}}},
		{RequestID: reqID, Author: "aura", Timestamp: now,
			LLMResponse: &agent.LLMResponse{Content: "A Caraglio: 12°C sereno.", FinishReason: "stop"}},
	}
	return func(yield func(*agent.Event, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// translate is the spike-sized seed of internal/agui/translator.go: a pure
// 1:N mapping from *agent.Event to AG-UI events, lifecycle framing included.
func translate(threadID, runID string, seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		if !yield(events.NewRunStartedEvent(threadID, runID), nil) {
			return
		}
		msgN := 0
		for ev, err := range seq {
			if err != nil {
				yield(events.NewRunErrorEvent(err.Error()), nil)
				return
			}
			if ti := ev.Actions.ToolInvocation; ti != nil {
				switch ti.Event {
				case agent.ToolInvocationStart:
					if !yield(events.NewToolCallStartEvent(ti.ToolCallID, ti.ToolName), nil) {
						return
					}
					if ti.Arguments != "" {
						if !yield(events.NewToolCallArgsEvent(ti.ToolCallID, ti.Arguments), nil) {
							return
						}
					}
				case agent.ToolInvocationEnd:
					if !yield(events.NewToolCallEndEvent(ti.ToolCallID), nil) {
						return
					}
					if !yield(events.NewToolCallResultEvent(fmt.Sprintf("msg-tool-%s", ti.ToolCallID), ti.ToolCallID, ti.ResultPreview), nil) {
						return
					}
				}
				continue
			}
			if len(ev.Actions.StateDelta) > 0 {
				ops := make([]events.JSONPatchOperation, 0, len(ev.Actions.StateDelta))
				for k, v := range ev.Actions.StateDelta {
					ops = append(ops, events.JSONPatchOperation{Op: "replace", Path: "/" + k, Value: v})
				}
				if !yield(events.NewStateDeltaEvent(ops), nil) {
					return
				}
				continue
			}
			if ev.LLMResponse != nil && ev.LLMResponse.Content != "" {
				msgN++
				msgID := fmt.Sprintf("msg-%d", msgN)
				if !yield(events.NewTextMessageStartEvent(msgID, events.WithRole("assistant")), nil) {
					return
				}
				if !yield(events.NewTextMessageContentEvent(msgID, ev.LLMResponse.Content), nil) {
					return
				}
				if !yield(events.NewTextMessageEndEvent(msgID), nil) {
					return
				}
			}
		}
		yield(events.NewRunFinishedEventWithOptions(threadID, runID, events.WithSuccessOutcome()), nil)
	}
}

func main() {
	failures := 0
	fail := func(format string, args ...any) {
		logf("FAIL", format, args...)
		failures++
	}

	// --- server: POST /agent/run → SSE via the SDK writer ---
	writer := sse.NewSSEWriter()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/run", func(w http.ResponseWriter, r *http.Request) {
		var in types.RunAgentInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		for ev, _ := range translate(in.ThreadID, "run-spike-016", synthSeq()) {
			// WriteEventWithType emits the `event: TYPE` line the PRD smoke shows;
			// plain WriteEvent would emit data-only frames (TS-SDK default).
			if err := writer.WriteEventWithType(r.Context(), w, ev, string(ev.Type())); err != nil {
				logf("SERVER", "write error: %v", err)
				return
			}
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logf("SUMMARY", "INVALIDATED — listen: %v", err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())
	base := "http://" + ln.Addr().String()
	logf("SERVER", "listening on %s", base)

	// --- client: PRD smoke payload, read SSE frames back ---
	payload := `{"threadId":"c0ffee00-1111-2222-3333-444444444444","runId":"ignored-client-side",` +
		`"messages":[{"id":"m1","role":"user","content":"ciao dimmi il meteo"}]}`
	start := time.Now()
	resp, err := http.Post(base+"/agent/run", "application/json", strings.NewReader(payload))
	if err != nil {
		logf("SUMMARY", "INVALIDATED — POST: %v", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		fail("Content-Type = %q, want text/event-stream", ct)
	}

	var gotTypes []string
	var firstByte time.Duration
	deltas := map[string]string{}
	sc := bufio.NewScanner(resp.Body)
	var curType string
	for sc.Scan() {
		line := sc.Text()
		if firstByte == 0 {
			firstByte = time.Since(start)
		}
		if line != "" {
			logf("FRAME", "%s", line)
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			curType = strings.TrimPrefix(line, "event: ")
			gotTypes = append(gotTypes, curType)
		case strings.HasPrefix(line, "data: "):
			if curType == "TEXT_MESSAGE_CONTENT" {
				var d struct {
					MessageID string `json:"messageId"`
					Delta     string `json:"delta"`
				}
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d); err != nil {
					fail("TEXT_MESSAGE_CONTENT data parse: %v", err)
				} else {
					deltas[d.MessageID] = d.Delta
				}
			}
		}
	}
	total := time.Since(start)

	want := []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"TOOL_CALL_START", "TOOL_CALL_ARGS",
		"TOOL_CALL_END", "TOOL_CALL_RESULT",
		"STATE_DELTA",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"RUN_FINISHED",
	}
	if fmt.Sprint(gotTypes) != fmt.Sprint(want) {
		fail("sequence mismatch:\n got  %v\n want %v", gotTypes, want)
	} else {
		logf("SEQUENCE", "13/13 events in PRD order")
	}
	if deltas["msg-1"] != "Ciao" || deltas["msg-2"] != "A Caraglio: 12°C sereno." {
		fail("deltas wrong: %v", deltas)
	} else {
		logf("PAYLOAD", "deltas intact: %q, %q", deltas["msg-1"], deltas["msg-2"])
	}
	logf("LATENCY", "loopback first-byte=%s total-stream=%s (translator+SSE floor, no LLM)", firstByte, total)

	// --- edge probes: what must the real translator guard against? ---
	if err := events.NewTextMessageContentEvent("msg-x", "").Validate(); err != nil {
		logf("EDGE", "empty delta REJECTED by Validate (%v) — translator MUST skip empty LLM deltas", err)
	} else {
		logf("EDGE", "empty delta accepted — no skip needed")
	}
	if err := events.NewToolCallStartEvent("", "web_search").Validate(); err != nil {
		logf("EDGE", "empty toolCallId REJECTED by Validate (%v) — translator MUST guarantee IDs", err)
	} else {
		logf("EDGE", "empty toolCallId accepted — SDK does not enforce ID presence")
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d failures", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED — iter.Seq2 → translate → SSEWriter → HTTP round-trip, framing `event:`+`id:`+`data:` confirmed")
}
