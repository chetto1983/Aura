package agui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

// connect_pim_account_api.go relays the mailbox repair flow to the aura-pim-mcp sidecar (spec
// 2026-11-20 pim-account-connect-repair): discovery of an address's servers, the verification of a
// stored account and the partial change of its provider config. Create, verify and PATCH make the
// sidecar log in to a mail server, so they share a per-identity limit (a wrong password is several
// login attempts, and providers lock accounts) and the private-hosts permission (amendment A5).

// pimDiscoverClient bounds discovery: the sidecar caps it at 12 s overall.
var pimDiscoverClient = &http.Client{Timeout: 15 * time.Second}

// pimVerifyClient bounds a call that logs in: an IMAP step of 12 s and an SMTP step of 8 s.
var pimVerifyClient = &http.Client{Timeout: 25 * time.Second}

// pimAllowPrivateHostsHeader lets the sidecar connect to a private address. Aura sets it, from the
// caller's capability alone; dialPIM never copies a browser's header.
const pimAllowPrivateHostsHeader = "X-Aura-Allow-Private-Hosts"

// pimAttemptsPerMinute is how many login attempts one identity may start in a minute.
const pimAttemptsPerMinute = 10

// pimAttemptLimiter counts each identity's login attempts over a sliding minute.
type pimAttemptLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	attempts map[string][]time.Time
}

func newPIMAttemptLimiter(now func() time.Time) *pimAttemptLimiter {
	return &pimAttemptLimiter{now: now, attempts: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it fits the identity's minute; a refused one is
// not recorded.
func (l *pimAttemptLimiter) allow(identityID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := slices.DeleteFunc(l.attempts[identityID], func(at time.Time) bool { return now.Sub(at) >= time.Minute })
	if len(recent) >= pimAttemptsPerMinute {
		l.attempts[identityID] = recent
		return false
	}
	l.attempts[identityID] = append(recent, now)
	return true
}

// handlePIMDiscover serves POST /api/connect/pim/discover: forward {email} to the sidecar's
// POST /admin/discover, which answers where the address's mail servers are. It reads public
// autoconfig and DNS and stores nothing.
func (s *Server) handlePIMDiscover(w http.ResponseWriter, r *http.Request) {
	body, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	s.forwardPIM(w, r, http.MethodPost, "/admin/discover", body, pimDiscoverClient, nil)
}

// handlePIMVerifyAccount serves POST /api/connect/pim/accounts/{id}/verify: the sidecar logs in
// with the stored config. A 422 names the failing step and reason and passes through unchanged.
func (s *Server) handlePIMVerifyAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	status := s.forwardPIMAttempt(w, r, http.MethodPost, "/admin/accounts/"+url.PathEscape(id)+"/verify", nil)
	s.wakeRepairedWatch(r, id, status)
}

// handlePIMPatchProviderConfig serves PATCH /api/connect/pim/accounts/{id}/provider-config: the
// sidecar verifies the merged config and stores it only if the login passes. Only providerConfig
// is forwarded, without the keys only Aura sets.
func (s *Server) handlePIMPatchProviderConfig(w http.ResponseWriter, r *http.Request) {
	body, ok := readCappedBody(w, r)
	if !ok || !s.pimWired(w) {
		return
	}
	var patch struct {
		ProviderConfig json.RawMessage `json:"providerConfig"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	config, err := providerConfigWithoutOwnedKeys(patch.ProviderConfig)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid providerConfig"})
		return
	}
	forwarded, err := json.Marshal(map[string]map[string]string{"providerConfig": config})
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "body encoding failed"})
		return
	}
	id := r.PathValue("id")
	status := s.forwardPIMAttempt(w, r, http.MethodPatch, "/admin/accounts/"+url.PathEscape(id)+"/provider-config", forwarded)
	s.wakeRepairedWatch(r, id, status)
}

// forwardPIMAttempt forwards a call that makes the sidecar log in to a mail server, after the
// caller's limit and through the long client, and returns the status it answered. Only an
// identity holding identity.create may name a private host.
func (s *Server) forwardPIMAttempt(w http.ResponseWriter, r *http.Request, method, path string, body []byte) int {
	if identityID, ok := principalIdentityID(r); ok && !s.pimAttempts.allow(identityID) {
		writeJSONStatus(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_attempts"})
		return http.StatusTooManyRequests
	}
	header := http.Header{}
	if s.callerIsAdmin(r) {
		header.Set(pimAllowPrivateHostsHeader, "true")
	}
	answer := &statusCapture{ResponseWriter: w}
	s.forwardPIM(answer, r, method, path, body, pimVerifyClient, header)
	return answer.status
}

// wakeRepairedWatch tells the mail watch an account works again once the sidecar accepted the
// repair. The repair is already stored, so a client that hung up must not keep the watch paused.
func (s *Server) wakeRepairedWatch(r *http.Request, accountID string, status int) {
	if s.mailWatch == nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	if err := s.mailWatch.Repaired(ctx, scopedIdentityID(ctx), accountID); err != nil {
		slog.Warn("agui: waking the repaired account's watch did not finish", "err", sanitizeErr(err))
	}
}
