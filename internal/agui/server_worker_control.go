package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
)

type workerSteerStore interface {
	PushWorker(context.Context, string, string, string, string) error
	WorkerHistory(context.Context, string, string) ([]steer.WorkerReceipt, error)
}

type workerControlRequest struct {
	RunID string `json:"run_id"`
	Text  string `json:"text,omitempty"`
}

func (s *Server) registerWorkerControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/conversations/{conv}/swarm/{child}/steer", s.handleWorkerSteer)
	mux.HandleFunc("POST /api/conversations/{conv}/swarm/{child}/cancel", s.handleWorkerCancel)
	mux.HandleFunc("GET /api/conversations/{conv}/swarm/{child}/controls", s.handleWorkerControlHistory)
}

func workerControlNotFound(w http.ResponseWriter) {
	http.Error(w, "worker not found", http.StatusNotFound)
}

func (s *Server) ownsWorkerConversation(w http.ResponseWriter, r *http.Request) bool {
	conv, child := r.PathValue("conv"), r.PathValue("child")
	if _, err := uuid.Parse(conv); err != nil || child == "" || len(child) > 128 || strings.ContainsAny(child, "/\\\x00") || s.conv == nil {
		workerControlNotFound(w)
		return false
	}
	if _, err := s.conv.GetForIdentity(r.Context(), conv, scopedIdentityID(r.Context())); err != nil {
		workerControlNotFound(w)
		return false
	}
	return true
}

func (s *Server) workerControlTarget(w http.ResponseWriter, r *http.Request) (*RunSession, workerControlRequest, bool) {
	var req workerControlRequest
	if !s.ownsWorkerConversation(w, r) {
		return nil, req, false
	}
	if s.runs == nil {
		workerControlNotFound(w)
		return nil, req, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRunBodyBytes+1))
	if err != nil || len(body) > maxRunBodyBytes || json.Unmarshal(body, &req) != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return nil, req, false
	}
	session, ok := s.runs.Get(req.RunID)
	if !ok || session.IdentityID != scopedIdentityID(r.Context()) || session.ThreadID != r.PathValue("conv") || session.WorkerID != r.PathValue("child") {
		workerControlNotFound(w)
		return nil, req, false
	}
	return session, req, true
}

func (s *Server) handleWorkerSteer(w http.ResponseWriter, r *http.Request) {
	session, req, ok := s.workerControlTarget(w, r)
	if !ok {
		return
	}
	store, ok := s.steer.(workerSteerStore)
	if !ok || !session.steerEnabled {
		workerControlNotFound(w)
		return
	}
	if err := session.withWorkerControl(func() error {
		return store.PushWorker(r.Context(), session.ThreadID, session.WorkerID, session.RunID, req.Text)
	}); err != nil {
		writeWorkerControlError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleWorkerCancel(w http.ResponseWriter, r *http.Request) {
	session, req, ok := s.workerControlTarget(w, r)
	if !ok {
		return
	}
	if req.Text != "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if session.operatorStop == nil {
		workerControlNotFound(w)
		return
	}
	if err := session.withWorkerControl(func() error { return session.operatorStop(r.Context()) }); err != nil {
		writeWorkerControlError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
}

func writeWorkerControlError(w http.ResponseWriter, err error) {
	if errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		writeSteerGone(w, "worker execution is no longer active")
		return
	}
	writeSteerRefusal(w, err)
}

func (s *Server) handleWorkerControlHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.ownsWorkerConversation(w, r) {
		return
	}
	store, ok := s.steer.(workerSteerStore)
	if !ok {
		workerControlNotFound(w)
		return
	}
	conv, child, owner := r.PathValue("conv"), r.PathValue("child"), scopedIdentityID(r.Context())
	if _, live := s.runs.LiveForWorker(owner, conv, child); !live {
		if s.swarmTranscripts == nil {
			workerControlNotFound(w)
			return
		}
		if _, _, err := s.swarmTranscripts.ReadTranscript(r.Context(), conv, child, 0); err != nil {
			workerControlNotFound(w)
			return
		}
	}
	ctx := identityctx.WithIdentityID(r.Context(), owner)
	receipts, err := store.WorkerHistory(ctx, conv, child)
	if err != nil {
		http.Error(w, "worker controls unavailable", http.StatusServiceUnavailable)
		return
	}
	for i := range receipts {
		if receipts[i].Status != "accepted" {
			continue
		}
		var session *RunSession
		var found bool
		if s.runs != nil {
			session, found = s.runs.Get(receipts[i].RunID)
		}
		if !found || session.IdentityID != owner || session.ThreadID != conv || session.WorkerID != child {
			receipts[i].Status, receipts[i].Reason = "rejected", "owner_unavailable"
		} else if terminal, _ := session.terminalState(); terminal {
			receipts[i].Status, receipts[i].Reason = "rejected", "worker_run_ended"
		}
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"receipts": receipts})
}
