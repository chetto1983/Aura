package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
)

type swarmWorkerStatusPayload struct {
	ChildID     string `json:"child_id"`
	Status      string `json:"status"`
	LastEventAt string `json:"last_event_at"`
	Events      int    `json:"events"`
	DurationSec int64  `json:"duration_sec"`
}

type swarmWorkerStatusState struct {
	offset       int64
	firstEventAt time.Time
	lastEventAt  time.Time
	lastEvent    *agent.Event
	events       int
	durationSec  int64
	emitted      bool
	lastPayload  swarmWorkerStatusPayload
}

func (s *Server) handleSwarmWorkerStatuses(w http.ResponseWriter, r *http.Request, conv string) {
	children, err := s.swarmTranscripts.ListChildTranscripts(r.Context(), conv)
	if err != nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	s.streamSSE(r.Context(), w, s.swarmWorkerStatusSequence(r.Context(), conv, children))
}

func (s *Server) swarmWorkerStatusSequence(ctx context.Context, conv string, initialChildren []string) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		states := make(map[string]*swarmWorkerStatusState)
		children := append([]string(nil), initialChildren...)
		for {
			sort.Strings(children)
			now := time.Now().UTC()
			for _, childID := range children {
				state := states[childID]
				if state == nil {
					state = &swarmWorkerStatusState{}
					states[childID] = state
				}
				chunk, nextOffset, err := s.swarmTranscripts.ReadTranscript(ctx, conv, childID, state.offset)
				if err != nil {
					yield(nil, err)
					return
				}
				state.offset = nextOffset
				state.ingest(childID, chunk, now)
				payload := state.payload(childID, now, s.swarmWorkerIdle)
				if swarmWorkerStatusChanged(state.lastPayload, payload, state.emitted) {
					if !yield(events.NewCustomEvent(SwarmWorkerEventName, events.WithValue(payload)), nil) {
						return
					}
					state.lastPayload = payload
					state.emitted = true
				}
			}

			timer := time.NewTimer(swarmTranscriptTailInterval)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
			listed, err := s.swarmTranscripts.ListChildTranscripts(ctx, conv)
			if err != nil {
				yield(nil, err)
				return
			}
			children = listed
		}
	}
}

func (s *swarmWorkerStatusState) ingest(childID string, chunk []byte, observedAt time.Time) {
	for line := range bytes.SplitSeq(chunk, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev agent.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Warn("agui: skipping malformed swarm status line", "child", childID, "err", err)
			continue
		}
		at := ev.Timestamp
		if at.IsZero() {
			at = observedAt
		}
		if s.firstEventAt.IsZero() {
			s.firstEventAt = at
		}
		s.lastEventAt = at
		s.lastEvent = &ev
		s.events++
		if duration, ok := swarmMarkerDuration(ev.Actions.StateDelta["swarm_child_duration_sec"]); ok {
			s.durationSec = duration
		} else if !s.firstEventAt.IsZero() && !s.lastEventAt.Before(s.firstEventAt) {
			s.durationSec = int64(s.lastEventAt.Sub(s.firstEventAt).Truncate(time.Second).Seconds())
		}
	}
}

func (s *swarmWorkerStatusState) payload(childID string, now time.Time, idle time.Duration) swarmWorkerStatusPayload {
	lastEventAt := ""
	if !s.lastEventAt.IsZero() {
		lastEventAt = s.lastEventAt.UTC().Format(time.RFC3339Nano)
	}
	return swarmWorkerStatusPayload{
		ChildID:     childID,
		Status:      deriveSwarmWorkerStatus(s.lastEvent, s.lastEventAt, now, idle),
		LastEventAt: lastEventAt,
		Events:      s.events,
		DurationSec: s.durationSec,
	}
}

func deriveSwarmWorkerStatus(last *agent.Event, lastEventAt, now time.Time, idle time.Duration) string {
	if status, ok := swarmTerminalStatus(last); ok {
		if isSwarmWorkerStatus(status) {
			return status
		}
		return display.StatusFailed
	}
	if last != nil && last.Actions.AwaitingInput != nil {
		return display.StatusNeedsUserInput
	}
	if idle > 0 && !lastEventAt.IsZero() && now.Sub(lastEventAt) > idle {
		return display.StatusStalled
	}
	return display.StatusRunning
}

func isSwarmWorkerStatus(status string) bool {
	switch status {
	case display.StatusOK,
		display.StatusFailed,
		display.StatusNeedsUserInput,
		display.StatusRunning,
		display.StatusStalled,
		display.StatusDeadLetter:
		return true
	default:
		return false
	}
}

func swarmMarkerDuration(value any) (int64, bool) {
	switch value := value.(type) {
	case json.Number:
		duration, err := value.Int64()
		return duration, err == nil && duration >= 0
	case int64:
		return value, value >= 0
	case int:
		return int64(value), value >= 0
	case float64:
		return int64(value), value >= 0
	default:
		return 0, false
	}
}

func swarmWorkerStatusChanged(previous, current swarmWorkerStatusPayload, emitted bool) bool {
	return !emitted || previous != current
}
