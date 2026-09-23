package agui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"

	"github.com/chetto1983/aura/internal/pimprovider"
)

// injectPIMProviderApp rewrites an account-create body before it reaches the sidecar. For a
// managed provider it replaces the browser's credentials with the admin-set app, so a member
// cannot choose the OAuth client. A non-zero status is the answer to send instead of forwarding.
//
// Aura mounts no account-update proxy. Any future one must run this same rewrite, because the
// sidecar's PUT replaces providerConfig wholesale.
func (s *Server) injectPIMProviderApp(ctx context.Context, body []byte) ([]byte, int, string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, http.StatusBadRequest, "invalid JSON body"
	}
	var provider string
	if err := json.Unmarshal(fields["provider"], &provider); err != nil || !pimprovider.Canonical(provider) {
		return nil, http.StatusBadRequest, "unknown provider"
	}
	if !pimprovider.Managed(provider) {
		return body, 0, ""
	}
	if s.pimApps == nil {
		return nil, http.StatusServiceUnavailable, "provider apps unavailable"
	}
	app, err := s.pimApps.Get(ctx, provider)
	if errors.Is(err, pimprovider.ErrNotConfigured) {
		return nil, http.StatusConflict, "provider_not_configured"
	}
	if err != nil {
		slog.Error("pim create: provider app read failed", "provider", provider, "err", err)
		return nil, http.StatusServiceUnavailable, "provider apps unavailable"
	}
	config := map[string]string{}
	if raw, ok := fields["providerConfig"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, http.StatusBadRequest, "invalid providerConfig"
		}
	}
	maps.DeleteFunc(config, func(key, _ string) bool { return pimprovider.IsOwnedKey(key) })
	maps.Copy(config, app.ProviderConfig())
	rewritten, err := json.Marshal(config)
	if err != nil {
		return nil, http.StatusInternalServerError, "providerConfig encoding failed"
	}
	fields["providerConfig"] = rewritten
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, http.StatusInternalServerError, "body encoding failed"
	}
	return out, 0, ""
}
