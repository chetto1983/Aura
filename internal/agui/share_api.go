// share_api.go owns the OWNER-SCOPED share CRUD surface (WEBSHARE-02/03, plan 37F-10):
// POST /api/shares (mint), GET /api/shares (list), PATCH /api/shares/{id}/snapshot
// (re-snapshot), DELETE /api/shares/{id} (revoke). Every read/mutate here is gated by
// RequireAuth (the parent-mux mount, cmd/aura/serve_webui_share.go, plan 37F-12) PLUS an
// owner predicate baked into the store query (share.Store's *ForIdentity methods) — ANY
// error, foreign or absent, collapses to 404, NEVER 403 (D-06). This is the OPPOSITE rule
// from share_api_internal.go's D-10 bearer-within-auth handlers in the SAME package: read
// that file's header before assuming this file's owner-gate-first shape generalises there.
//
// registerShareRoutes is the single entry point server.go's Mux() calls; it registers this
// file's four owner routes directly and delegates the remaining four to
// registerShareInternalRoutes (share_api_internal.go) and registerSharePublicRoutes
// (share_api_public.go) — so every one of the plan's eight route strings lives beside the
// handler it drives, never centralised in share_service.go.
package agui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/share"
)

// registerShareRoutes mounts the eight WEBSHARE-02/03 share-lifecycle routes across three
// trust boundaries. The parent-mux mount (RequireAuth whole-origin, RequireCapability(
// share.public) on the public-tier mint, and the isPublicShareRoute allowlist admitting
// /s/... unauthenticated) lives in cmd/aura/serve_webui_share.go (plan 37F-12) — none of that
// wiring is in this file.
func (s *Server) registerShareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/shares", s.handleShareCreate)
	mux.HandleFunc("GET /api/shares", s.handleShareList)
	mux.HandleFunc("PATCH /api/shares/{id}/snapshot", s.handleShareUpdateSnapshot)
	mux.HandleFunc("DELETE /api/shares/{id}", s.handleShareRevoke)
	s.registerShareInternalRoutes(mux)
	s.registerSharePublicRoutes(mux)
}

// shareCreateBody is POST /api/shares' request shape — the wire contract plan 37F-15's
// shareApi.ts locked (CreateShareBody): conversation_id + tier are required, expiry_option/
// custom_expiry_days are optional (an absent expiry_option resolves to share.ExpiryDefault,
// D-04). OwnerIdentityID is deliberately NOT a body field — it comes from the authenticated
// principal, never from client input.
type shareCreateBody struct {
	ConversationID   string             `json:"conversation_id"`
	Tier             share.Tier         `json:"tier"`
	ExpiryOption     share.ExpiryOption `json:"expiry_option"`
	CustomExpiryDays int                `json:"custom_expiry_days"`
}

// shareLinkResponse mirrors web/src/chat/share/shareTypes.ts's ShareLink (plan 37F-15) — the
// API view of one aura.shared_links row. It is NOT share.Link projected verbatim: Link
// additionally carries OwnerIdentityID/ConversationID/SnapshotBucket/FormatOptions, none of
// which belong on the wire, and Link carries no derived url at all.
type shareLinkResponse struct {
	ID                string     `json:"id"`
	Tier              string     `json:"tier"`
	URL               string     `json:"url,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	SnapshotTurnCount *int       `json:"snapshot_turn_count,omitempty"`
}

// toShareLinkResponse projects link (+ the one-time plaintext token, present ONLY from a
// fresh public-tier Create) to the shareTypes.ts ShareLink shape. An internal link's url is
// ALWAYS the server-derived /shared/{id} form — no secret behind it, migration 0040 forces
// token_hash IS NULL for tier='internal' — so it is present even from handleShareList. A
// public link's url is the one-time plaintext /s/{token} form and is present ONLY here, at
// mint: every other response (list, update) omits it — there is nothing to re-derive a hashed
// token back into. token MUST be "" for anything but a fresh public-tier Create.
func toShareLinkResponse(link share.Link, token string) shareLinkResponse {
	resp := shareLinkResponse{
		ID:        link.ID.String(),
		Tier:      link.Tier,
		CreatedAt: link.CreatedAt,
		UpdatedAt: link.UpdatedAt,
		ExpiresAt: link.ExpiresAt,
		RevokedAt: link.RevokedAt,
	}
	switch {
	case link.Tier == string(share.TierInternal):
		resp.URL = "/shared/" + resp.ID
	case token != "":
		resp.URL = "/s/" + token
	}
	return resp
}

// handleShareCreate mints a new share link for the authenticated caller's own conversation
// (D-06 owner gate runs inside share.Service.Create, first, before any side effect).
func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body shareCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// R-08 closure: RequireCapability (auth.go:282) returns `next` unchanged when
	// !SecretConfigured, so on loopback dev the share.public capability gate at the mount does
	// not structurally exist. s.cfg.SharePublicEnabled is re-checked HERE, inside the handler,
	// where no bypass applies — the gate that survives loopback. Two gates, both fail-closed;
	// share.Service.Create ALSO re-checks the identical config value (ErrSharePublicDisabled
	// below), belt-and-suspenders with the service layer.
	if body.Tier == share.TierPublic && !s.cfg.SharePublicEnabled {
		http.Error(w, "public sharing is disabled", http.StatusForbidden)
		return
	}

	res, err := s.share.Create(r.Context(), share.CreateRequest{
		ConversationID:   body.ConversationID,
		OwnerIdentityID:  identityID,
		Tier:             body.Tier,
		ExpiryOption:     body.ExpiryOption,
		CustomExpiryDays: body.CustomExpiryDays,
	})
	if err != nil {
		switch {
		case errors.Is(err, share.ErrShareNotFound):
			http.Error(w, sanitizeErr(err), http.StatusNotFound)
		case errors.Is(err, share.ErrSharePublicDisabled):
			http.Error(w, sanitizeErr(err), http.StatusForbidden)
		case errors.Is(err, share.ErrNonPositiveCustomExpiry):
			http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		default:
			http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		}
		return
	}
	// The plaintext token appears HERE and nowhere else (D-13): no log line, no share_audit
	// column, no second response ever carries it again.
	writeJSONStatus(w, http.StatusCreated, toShareLinkResponse(res.Link, res.Token))
}

// handleShareList returns the caller's own links, optionally scoped to one conversation via
// ?conversation_id= (the "Condiviso" filter, 37F-17).
func (s *Server) handleShareList(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var links []share.Link
	var err error
	if convID := r.URL.Query().Get("conversation_id"); convID != "" {
		links, err = s.share.ListForConversation(r.Context(), convID, identityID)
	} else {
		limit := parseLimit(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		links, err = s.share.ListForIdentity(r.Context(), identityID, limit, offset)
	}
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	out := make([]shareLinkResponse, 0, len(links))
	for _, l := range links {
		// No token on any list row (D-13): a public link's plaintext is never re-derivable
		// from its hash, so "" here is the only correct value, not a shortcut.
		out = append(out, toShareLinkResponse(l, ""))
	}
	writeJSON(w, out)
}

// handleShareUpdateSnapshot re-snapshots an owner-scoped share, keeping the same token
// (D-06). ANY error — foreign id, absent id, store failure — is 404, never 403 (SC4 row 6): a
// foreign share id must be indistinguishable from a missing one.
func (s *Server) handleShareUpdateSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	link, err := s.share.UpdateSnapshot(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	writeJSON(w, toShareLinkResponse(link, ""))
}

// handleShareRevoke revokes an owner-scoped share and drops its object-store blobs. ANY error
// is 404, never 403 (SC4 row 6) — see handleShareUpdateSnapshot's doc.
func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.share.Revoke(r.Context(), r.PathValue("id"), identityID); err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
