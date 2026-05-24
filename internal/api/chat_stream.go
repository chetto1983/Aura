package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	webadapter "github.com/aura/aura/internal/channels/web"
)

type chatStreamRequest struct {
	UserID        string              `json:"user_id"`
	Message       string              `json:"message"`
	ThreadID      string              `json:"thread_id"`
	ThreadIDCamel string              `json:"threadId"`
	Messages      []chatStreamMessage `json:"messages"`
}

type chatStreamMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Parts   json.RawMessage `json:"parts"`
	Text    string          `json:"text"`
}

func handleChatStream(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.ChatStream == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "chat stream service is not configured")
			return
		}
		var req chatStreamRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = strings.TrimSpace(lastUserText(req.Messages))
		}
		if message == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "message is required")
			return
		}
		threadID := strings.TrimSpace(req.ThreadID)
		if threadID == "" {
			threadID = strings.TrimSpace(req.ThreadIDCamel)
		}
		if threadID == "" {
			threadID = "default"
		}
		chatCtx, userID, ok := authorizeChatRequest(w, r, deps, req.UserID)
		if !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, deps.Logger, http.StatusInternalServerError, "streaming is not supported")
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream; charset=utf-8")
		h.Set("Cache-Control", "no-cache, no-transform")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		h.Set(webadapter.UIMessageStreamHeader, webadapter.UIMessageStreamHeaderValue)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		err := deps.ChatStream.ChatStream(chatCtx, userID, threadID, message, w, flusher.Flush)
		if err != nil && !errors.Is(err, r.Context().Err()) && deps.Logger != nil {
			deps.Logger.Warn("api: chat stream failed", "error", err)
		}
	}
}

func lastUserText(messages []chatStreamMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.Role) != "user" {
			continue
		}
		if text := strings.TrimSpace(msg.Text); text != "" {
			return text
		}
		if text := strings.TrimSpace(textFromRawJSON(msg.Content)); text != "" {
			return text
		}
		if text := strings.TrimSpace(textFromRawJSON(msg.Parts)); text != "" {
			return text
		}
	}
	return ""
}

func textFromRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if part.Type == "" || part.Type == "text" {
				if b.Len() > 0 && part.Text != "" {
					b.WriteByte('\n')
				}
				b.WriteString(part.Text)
			}
		}
		return b.String()
	}
	var obj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Text
	}
	return ""
}
