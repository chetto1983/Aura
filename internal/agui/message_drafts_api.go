package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type messageDraftRunner interface {
	ListMessageDrafts(context.Context, string) ([]messagedrafts.Draft, error)
	ResolveMessageDraft(context.Context, string, string, json.RawMessage) (runner.MessageDraftResolution, error)
}

type messageDraftItem struct {
	ID             string               `json:"id"`
	ConversationID string               `json:"conversation_id"`
	Channel        string               `json:"channel"`
	Status         messagedrafts.Status `json:"status"`
	Arguments      json.RawMessage      `json:"arguments"`
	EffectiveArgs  json.RawMessage      `json:"effective_arguments,omitempty"`
	ExpiresAt      time.Time            `json:"expires_at"`
}

func projectMessageDraft(draft messagedrafts.Draft) messageDraftItem {
	channel := "email"
	if draft.Target.Recipe == "recipe:whatsapp" {
		channel = "whatsapp"
	}
	status := draft.Status
	if status == messagedrafts.StatusPending && !draft.ExpiresAt.After(time.Now()) {
		status = messagedrafts.StatusExpired
	}
	return messageDraftItem{
		ID: draft.ID, ConversationID: draft.ConversationID, Channel: channel,
		Status: status, Arguments: reviewMessageArgs(draft.OriginalArgs), EffectiveArgs: reviewMessageArgs(draft.EffectiveArgs),
		ExpiresAt: draft.ExpiresAt,
	}
}

// Attachment bytes and opaque storage identifiers stay in the server-side call.
// The review only needs their human-readable metadata and count.
func reviewMessageArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return json.RawMessage(`{}`)
	}
	if encoded, ok := args["attachments"]; ok {
		var attachments []map[string]json.RawMessage
		if json.Unmarshal(encoded, &attachments) != nil {
			delete(args, "attachments")
		} else {
			for _, attachment := range attachments {
				delete(attachment, "base64Content")
				delete(attachment, "attachmentId")
			}
			metadata, err := json.Marshal(attachments)
			if err != nil {
				delete(args, "attachments")
			} else {
				args["attachments"] = metadata
			}
		}
	}
	projected, err := json.Marshal(args)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return projected
}

func (s *Server) registerMessageDraftRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/message-drafts", s.handleListMessageDrafts)
	mux.HandleFunc("POST /api/message-drafts/{id}/resolve", s.handleResolveMessageDraft)
}

func (s *Server) handleListMessageDrafts(w http.ResponseWriter, r *http.Request) {
	service, ok := s.run.(messageDraftRunner)
	if !ok {
		http.Error(w, "message review unavailable", http.StatusServiceUnavailable)
		return
	}
	conversationID := r.URL.Query().Get("conversation_id")
	if _, err := uuid.Parse(conversationID); err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	ctx := scopedCtx(r.Context())
	if _, err := s.conv.GetForIdentity(ctx, conversationID, scopedIdentityID(r.Context())); err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	drafts, err := service.ListMessageDrafts(ctx, conversationID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	items := make([]messageDraftItem, 0, len(drafts))
	for _, draft := range drafts {
		items = append(items, projectMessageDraft(draft))
	}
	writeJSON(w, items)
}

type messageDraftResolveBody struct {
	Action    string          `json:"action"`
	Overrides json.RawMessage `json:"overrides"`
}

func (s *Server) handleResolveMessageDraft(w http.ResponseWriter, r *http.Request) {
	service, ok := s.run.(messageDraftRunner)
	if !ok {
		http.Error(w, "message review unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "message review not found", http.StatusNotFound)
		return
	}
	var body messageDraftResolveBody
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024+1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) ||
		(body.Action != "send" && body.Action != "decline") ||
		(body.Action == "decline" && len(body.Overrides) != 0) {
		http.Error(w, "invalid message review decision", http.StatusBadRequest)
		return
	}
	resolution, err := service.ResolveMessageDraft(scopedCtx(r.Context()), id, body.Action, body.Overrides)
	if err != nil {
		switch {
		case errors.Is(err, messagedrafts.ErrUnavailable):
			http.Error(w, "message review not found or already resolved", http.StatusNotFound)
		case errors.Is(err, messagedrafts.ErrInvalidArgs):
			http.Error(w, "invalid message edits", http.StatusBadRequest)
		case errors.Is(err, runner.ErrThreadBusy):
			http.Error(w, "conversation busy", http.StatusConflict)
		default:
			http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, struct {
		Status    messagedrafts.Status `json:"status"`
		Outcome   string               `json:"outcome"`
		Remaining int                  `json:"remaining"`
	}{Status: resolution.Status, Outcome: outcomeString(resolution.Directive.Outcome), Remaining: resolution.Directive.Remaining})
}
