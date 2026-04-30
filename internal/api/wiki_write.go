package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

// wikiWriter is the write surface needed by the rebuild + log endpoints.
// Kept narrow so a future Deps refactor doesn't have to widen WikiStore
// for the read path.
type wikiWriter interface {
	RebuildIndex(ctx context.Context)
	AppendLog(ctx context.Context, action, slug string)
}

// AppendLogRequest is the JSON body for POST /wiki/log. Slug is optional —
// actions like "lint" or "query" don't pertain to a single page.
type AppendLogRequest struct {
	Action string `json:"action"`
	Slug   string `json:"slug,omitempty"`
}

// logActionRe restricts the action label to a conservative shell-safe set
// so a malicious caller can't smuggle newlines into log.md.
var logActionRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,32}$`)

func handleWikiRebuild(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writer, ok := deps.Wiki.(wikiWriter)
		if !ok {
			writeError(w, deps.Logger, http.StatusInternalServerError, "wiki store does not support writes")
			return
		}
		writer.RebuildIndex(r.Context())
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleWikiAppendLog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AppendLogRequest
		if err := decodeJSONBody(r, &req); err != nil {
			if errors.Is(err, io.EOF) {
				writeError(w, deps.Logger, http.StatusBadRequest, "request body required")
				return
			}
			writeError(w, deps.Logger, http.StatusBadRequest, "parse json: "+err.Error())
			return
		}
		req.Action = strings.TrimSpace(req.Action)
		req.Slug = strings.TrimSpace(req.Slug)
		if req.Action == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "action required")
			return
		}
		if !logActionRe.MatchString(req.Action) {
			writeError(w, deps.Logger, http.StatusBadRequest, "action must be 1-32 chars [A-Za-z0-9_.-]")
			return
		}
		if req.Slug != "" && wiki.Slug(req.Slug) != req.Slug {
			writeError(w, deps.Logger, http.StatusBadRequest, "slug must be canonical (lowercase, hyphens)")
			return
		}
		writer, ok := deps.Wiki.(wikiWriter)
		if !ok {
			writeError(w, deps.Logger, http.StatusInternalServerError, "wiki store does not support writes")
			return
		}
		writer.AppendLog(r.Context(), req.Action, req.Slug)
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true})
	}
}
