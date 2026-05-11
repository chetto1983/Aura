package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aura/aura/internal/auth"
)

// userIDFromRequest returns the authenticated user ID injected by the bearer
// middleware, or "" when the router runs unauthenticated (tests).
func userIDFromRequest(r *http.Request) string {
	return auth.UserIDFromContext(r.Context())
}

// ChatService is the in-process bridge the /chat endpoint uses to drive an
// agent turn. It is intentionally narrow: one message in, one reply out, no
// streaming, no Telegram coupling. cmd/aura wires this against agent.Runner
// so the chat pipe shares the live LLM client + tool registry the bot uses.
type ChatService interface {
	Chat(ctx context.Context, userID, message string) (ChatReply, error)
}

// ChatReply is the JSON shape the endpoint returns. Stats fields mirror the
// agent.Runner stats so a CLI client can show progress without parsing logs.
type ChatReply struct {
	Reply     string `json:"reply"`
	ElapsedMs int64  `json:"elapsed_ms"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
	Tokens    int    `json:"tokens"`
}

type chatRequest struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

// handleChat is the POST /chat handler. The agent runs synchronously and the
// reply lands on the response body in one shot — chat pipe callers do not
// need streaming.
func handleChat(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Chat == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "chat service is not configured")
			return
		}
		var req chatRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "message is required")
			return
		}
		userID := strings.TrimSpace(req.UserID)
		if userID == "" {
			// Default to the authenticated user when present. The auth
			// middleware injects the user ID into the context.
			userID = userIDFromRequest(r)
		}
		if userID == "" {
			userID = "chat-cli"
		}
		reply, err := deps.Chat.Chat(r.Context(), userID, message)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				writeError(w, deps.Logger, http.StatusRequestTimeout, "client cancelled the chat request")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "chat failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, reply)
	}
}
