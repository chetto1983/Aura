package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"log/slog"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/google/uuid"
)

// The server tails worker transcripts because delegation status is push-only at
// the cockpit boundary; exposing a cursor would force the browser to poll.
const swarmTranscriptTailInterval = time.Second

// SetSwarmWorkerIdle sets the shared inactivity threshold used by the worker
// transcript and status streams. A non-positive duration disables idle expiry.
func (s *Server) SetSwarmWorkerIdle(idle time.Duration) { s.swarmWorkerIdle = idle }

func (s *Server) registerSwarmWorkerEventRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations/{conv}/swarm/events", s.handleSwarmWorkerEvents)
}

func (s *Server) handleSwarmWorkerEvents(w http.ResponseWriter, r *http.Request) {
	conv := r.PathValue("conv")
	if _, err := uuid.Parse(conv); err != nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if _, err := s.conv.GetForIdentity(ctx, conv, scopedIdentityID(ctx)); err != nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	if s.swarmTranscripts == nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	childID := r.URL.Query().Get("child")
	if childID == "" {
		s.handleSwarmWorkerStatuses(w, r, conv)
		return
	}

	// Preflight before committing SSE headers so a rejected child id is
	// indistinguishable from every other failure in the opaque 404 ladder.
	body, offset, err := s.swarmTranscripts.ReadTranscript(ctx, conv, childID, 0)
	if err != nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	stream := Translate(
		conv+"-swarm-"+childID,
		childID,
		s.idgen,
		s.swarmChildEventSequence(ctx, conv, childID, body, offset),
		false,
	)
	s.streamSSE(ctx, w, stream)
}

func (s *Server) swarmChildEventSequence(
	ctx context.Context,
	conv string,
	childID string,
	body []byte,
	offset int64,
) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		lastGrowth := time.Now()
		emit := func(chunk []byte) (terminal, keepGoing bool) {
			for line := range bytes.SplitSeq(chunk, []byte{'\n'}) {
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				var ev agent.Event
				if err := json.Unmarshal(line, &ev); err != nil {
					slog.Warn("agui: skipping malformed swarm transcript line", "child", redact.Line(childID), "err", err)
					continue
				}
				if !yield(&ev, nil) {
					return false, false
				}
				if _, ok := swarmTerminalStatus(&ev); ok {
					return true, true
				}
			}
			return false, true
		}

		if len(body) > 0 {
			terminal, keepGoing := emit(body)
			if terminal || !keepGoing {
				return
			}
		}

		for {
			wait := swarmTranscriptTailInterval
			if s.swarmWorkerIdle > 0 {
				remaining := s.swarmWorkerIdle - time.Since(lastGrowth)
				if remaining <= 0 {
					yield(swarmIdleTerminalEvent(childID), nil)
					return
				}
				if remaining < wait {
					wait = remaining
				}
			}

			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}

			chunk, nextOffset, err := s.swarmTranscripts.ReadTranscript(ctx, conv, childID, offset)
			if err != nil {
				yield(nil, err)
				return
			}
			offset = nextOffset
			if len(chunk) == 0 {
				continue
			}
			lastGrowth = time.Now()
			terminal, keepGoing := emit(chunk)
			if terminal || !keepGoing {
				return
			}
		}
	}
}

func swarmTerminalStatus(ev *agent.Event) (string, bool) {
	if ev == nil || ev.Actions.StateDelta == nil {
		return "", false
	}
	status, ok := ev.Actions.StateDelta["swarm_child_status"].(string)
	return status, ok && status != ""
}

func swarmIdleTerminalEvent(childID string) *agent.Event {
	return &agent.Event{Actions: agent.Actions{StateDelta: map[string]any{
		"swarm_child_id":     childID,
		"swarm_child_status": "stalled",
	}}}
}
