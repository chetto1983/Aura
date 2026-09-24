package agui

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/chetto1983/aura/internal/conversations"
)

// handleConversationExport serves the owner's raw conversation dump,
// GET /api/conversations/{id}/export (prd.md §7). It rides the conversationsRoutePrefix
// mount, so RequireAuth is inherited whole-origin; the route is never public. Share links
// do NOT come through here: a token holder only ever reaches the redacted share.Snapshot.
//
//   - Existence hiding: GetForIdentity's ownership gate runs BEFORE any history read, and
//     any error it returns — foreign owner, absent or malformed id — is the same 404.
//   - Stored-XSS guard: the body is user- and model-authored text, so it is served as
//     application/octet-stream with nosniff, never as a renderable MIME type.
//   - Header-injection guard: the filename goes through contentDisposition, never
//     concatenated into the header by hand.
func (s *Server) handleConversationExport(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.assetCaller(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")

	conv, err := s.conv.GetForIdentity(r.Context(), id, identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	dump, err := s.conv.LoadDump(scopedCtx(r.Context()), id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	threadAssets, err := s.assets.ListForThread(r.Context(), identityID, id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	listed := make([]conversations.DumpAsset, 0, len(threadAssets))
	for _, a := range threadAssets {
		listed = append(listed, conversations.DumpAsset{
			ID: a.ID, FileName: a.FileName, MIMEType: a.MIMEType, SizeBytes: a.SizeBytes,
			SourceKind: string(a.SourceKind), Status: string(a.Status),
		})
	}

	body := dump.Markdown(conv, listed, time.Now().UTC())
	setAttachmentHeaders(w.Header(), exportFilenameStem(conv.Title)+".md", int64(len(body)))
	_, _ = w.Write(body)
}

// exportFilenameStem slugifies a conversation title into a readable
// download-filename stem: unicode letters/digits are kept and lowercased, any
// other rune (whitespace, punctuation) collapses to a single hyphen, and
// leading/trailing hyphens are trimmed. An empty or fully-punctuation title
// falls back to "conversation" so every export still gets a legible name.
// This function only improves readability — it is NOT the safety boundary:
// contentDisposition is what makes the final header injection- and
// traversal-safe (T-37F-14) regardless of what this returns.
func exportFilenameStem(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	lastHyphen := true // suppresses a leading hyphen
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	stem := strings.TrimSuffix(b.String(), "-")
	if stem == "" {
		return "conversation"
	}
	return stem
}
