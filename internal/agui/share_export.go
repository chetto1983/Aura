package agui

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/chetto1983/aura/internal/share"
)

// share_export.go ships WEBSHARE-01: the owner-scoped conversation export,
// GET /api/conversations/{id}/export?format=md|json. Unlike the public share
// handler (plan 37F-10), this route needs NO mount, NO const, and NO
// cmd/aura/serve_webui.go edit (F-1): it rides the SAME conversationsRoutePrefix
// mux.Handle already registered at serve_webui.go:381, so it inherits
// RequireAuth whole-origin from that mount. There is no per-route auth wiring
// here, and this route is never added to any public-path allowlist.
//
// handleConversationExport is the security invariant surface:
//   - D-06 existence-hiding: GetForIdentity's ownership gate runs BEFORE any
//     history read, and ANY error it returns — foreign owner or absent id —
//     collapses to the SAME 404. A read must never confirm a foreign
//     conversation's existence through a distinguishable status (SC4 row 1).
//   - 37A D-10 stored-XSS guard: the serve Content-Type is the neutral
//     application/octet-stream plus X-Content-Type-Options: nosniff for BOTH
//     export formats — never a format-specific MIME — because the exported
//     bytes are user-authored text a browser could otherwise try to render.
//   - D-11 header-injection guard: the filename rides Content-Disposition via
//     contentDisposition (RFC-6266 + percent-encoding); it is never
//     concatenated into the header by hand.
//   - One redacted snapshot, two adapters: MD and JSON both derive from the
//     SAME constructor call, so redaction cannot diverge between formats —
//     there is no second serializer.
//   - An absent or unrecognized format degrades to Markdown rather than
//     rejecting the request: a human clicking "export" wants the readable
//     form, and an unrecognized optional query value is not a client error.
func (s *Server) handleConversationExport(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")

	// Owner gate FIRST (D-06 / SC4 row 1): ANY error — a foreign owner, an
	// absent id, or a malformed one — collapses to the same 404 below. Only
	// once this succeeds does the unscoped history read run.
	conv, err := s.conv.GetForIdentity(r.Context(), id, identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}

	history, err := s.conv.LoadHistory(scopedCtx(r.Context()), id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	rawArtifacts, err := s.assets.ListForThread(r.Context(), identityID, id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	filtered := share.BundleFilter(rawArtifacts)
	metas := make([]share.ArtifactMeta, 0, len(filtered))
	for _, a := range filtered {
		metas = append(metas, share.ArtifactMeta{
			AssetID:   a.ID,
			FileName:  a.FileName,
			MIMEType:  a.MIMEType,
			SizeBytes: a.SizeBytes,
		})
	}

	// conv.CreatedAt is the store's RFC3339 string projection (store.go); a parse
	// failure degrades to the zero time rather than failing the export — the same
	// discard-on-error shape internal/share's own composition-root-shaped adapter
	// uses (service_integration_test.go's convReaderAdapter).
	createdAt, _ := time.Parse(time.RFC3339, conv.CreatedAt)
	snap, err := share.BuildSnapshot(
		share.ConvMeta{Title: conv.Title, Model: conv.Model, CreatedAt: createdAt},
		history, metas, time.Now().UTC(),
	)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}

	// One redacted snapshot, two format adapters — never a second serializer.
	// Absent or unrecognized format defaults to Markdown; only the exact "json"
	// token switches the other way.
	var body []byte
	ext := "md"
	if r.URL.Query().Get("format") == "json" {
		data, jErr := snap.JSON()
		if jErr != nil {
			http.Error(w, sanitizeErr(jErr), http.StatusInternalServerError)
			return
		}
		body, ext = data, "json"
	} else {
		body = snap.Markdown()
	}

	filename := exportFilenameStem(conv.Title) + "." + ext

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition(filename))
	h.Set("Content-Length", strconv.Itoa(len(body)))
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
