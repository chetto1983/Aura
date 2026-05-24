package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/identity"
)

// userIDFromRequest returns the authenticated user ID injected by the bearer
// middleware, or "" when the router runs unauthenticated (tests).
func userIDFromRequest(r *http.Request) string {
	return auth.UserIDFromContext(r.Context())
}

func actorIDFromRequest(r *http.Request) string {
	return auth.ActorIDFromContext(r.Context())
}

// ChatService is the in-process bridge the /chat endpoint uses to drive an
// agent turn. It is intentionally narrow: one message in, one reply out, no
// streaming, no Telegram coupling. cmd/aura wires this via agent.RunTask
// so the chat pipe shares the live LLM client + tool registry the bot uses.
type ChatService interface {
	Chat(ctx context.Context, userID, threadID, message string) (ChatReply, error)
}

// ChatStreamService drives the streaming /chat/stream endpoint. The handler
// owns HTTP auth and headers; the channel adapter owns UI message frames.
type ChatStreamService interface {
	ChatStream(ctx context.Context, userID, threadID, message string, w io.Writer, flush func()) error
}

// ChatReply is the JSON shape the endpoint returns. Stats fields mirror the
// agent.Result fields so a CLI client can show progress without parsing logs.
type ChatReply struct {
	Reply     string `json:"reply"`
	ElapsedMs int64  `json:"elapsed_ms"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
	Tokens    int    `json:"tokens"`
	// CacheHit is true when at least one prompt token was served from the
	// provider's KV cache during this turn. Signals that the static prefix
	// was successfully cached, which reduces both latency and input cost.
	CacheHit bool `json:"cache_hit,omitempty"`
	// ToolsUsed lists the unique tool names invoked during the run, in the
	// order first seen. Populated from EventToolStart events by the web
	// outbound adapter. Used by quality-bench to verify tool-selection
	// (e.g., did Aura call web_search when only wiki tools were appropriate?).
	// Empty when no tools were called or the channel doesn't track tool names.
	ToolsUsed     []string `json:"tools_used,omitempty"`
	BudgetWarning string   `json:"budget_warning,omitempty"`
}

// ChatRequest is the POST /chat request body.
type ChatRequest struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
	// ThreadID scopes the agent_note scratchpad to a named conversation thread.
	// Omitting it is equivalent to "default".
	ThreadID string `json:"thread_id,omitempty"`
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
		var req ChatRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "message is required")
			return
		}
		threadID := strings.TrimSpace(req.ThreadID)
		if threadID == "" {
			threadID = "default"
		}
		chatCtx, userID, ok := authorizeChatRequest(w, r, deps, req.UserID)
		if !ok {
			return
		}
		reply, err := deps.Chat.Chat(chatCtx, userID, threadID, message)
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

func authorizeChatRequest(w http.ResponseWriter, r *http.Request, deps Deps, bodyUserID string) (context.Context, string, bool) {
	authUserID := userIDFromRequest(r)
	chatCtx := r.Context()
	userID := authUserID
	if authUserID != "" {
		if bodyUserID := strings.TrimSpace(bodyUserID); bodyUserID != "" && bodyUserID != authUserID {
			writeError(w, deps.Logger, http.StatusForbidden, "authenticated user_id override is forbidden")
			return nil, "", false
		}
		decision, err := deps.Auth.Authorize(r.Context(), identity.AuthorizeParams{
			ActorID:    actorIDFromRequest(r),
			Capability: identity.CapabilityAPIChat,
			Resource:   identity.ResourceRef{Type: "api", ID: "chat"},
		})
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "authorization failed")
			return nil, "", false
		}
		if decision.Decision != identity.DecisionAllow {
			writeError(w, deps.Logger, http.StatusForbidden, "missing api.chat grant")
			return nil, "", false
		}
		chatCtx = identity.WithAuthority(chatCtx, deps.Auth)
	} else {
		userID = strings.TrimSpace(bodyUserID)
	}
	if userID == "" {
		userID = "chat-cli"
	}
	return chatCtx, userID, true
}
