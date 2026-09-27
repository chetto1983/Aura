package agui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/chetto1983/aura/internal/elicit"
)

// server_run_elicitation.go carries the operator's side of a mounted MCP server's
// form (run_elicitation.go): POST /agent/runs/{runID}/elicitations/{id} answers
// one, GET /agent/runs/{runID}/elicitations lists those still open. Both resolve
// the run through the same owner-scoped 404 ladder as steer and cancel.

type elicitationAnswerRequest struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content"`
}

var elicitationActions = []string{elicit.ActionAccept, elicit.ActionDecline, elicit.ActionCancel}

func (s *Server) handleRunElicitation(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	if terminal, _ := sess.terminalState(); terminal {
		http.Error(w, "run has ended", http.StatusGone)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRunBodyBytes+1))
	if err != nil || len(body) > maxRunBodyBytes {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var req elicitationAnswerRequest
	if err := json.Unmarshal(body, &req); err != nil || !slices.Contains(elicitationActions, req.Action) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// The 422 carries elicit's problem codes and never the submitted value: the
	// idempotency layer keeps this body for its replay (idempotency_http.go).
	fieldErrs, err := sess.questions.answer(r.PathValue("id"), elicit.Answer{Action: req.Action, Content: req.Content})
	switch {
	case errors.Is(err, errQuestionUnknown):
		http.Error(w, "question not found", http.StatusNotFound)
	case errors.Is(err, errQuestionClosed):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case fieldErrs != nil:
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"errors": fieldErrs})
	default:
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "delivered"})
	}
}

// handleRunElicitations lists the run's open questions, oldest first. A reattach
// whose replay the ring can no longer serve gets a 410 on /events and no frame at
// all, so the cockpit reads the forms from here. A read, like /events.
func (s *Server) handleRunElicitations(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"questions": sess.questions.open()})
}
