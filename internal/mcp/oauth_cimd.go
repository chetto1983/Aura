package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

// oauth_cimd.go signs Aura in where the authorization server accepts no client Aura could
// otherwise present: no operator-registered client, and no registration endpoint to mint
// one. What such a server may still accept is a Client ID Metadata Document
// (draft-ietf-oauth-client-id-metadata-document-00), where the client_id is the HTTPS URL
// of a JSON document describing the client.
//
// Measured 2026-09-23 against ElevenLabs' hosted MCP, the server that needed this: its
// authorization metadata advertises client_id_metadata_document_supported and no
// registration_endpoint; it fetches a client_id URL on any domain; and with Aura's
// published document it shows a consent screen naming "Aura". What was NOT measured there
// is the refresh leg, which depends on the provider honouring the refresh token it issues.
//
// It is a FALLBACK, never the first choice. Linear and Notion advertise metadata documents
// too, and they sign in today through dynamic registration on the caller's own redirect;
// trying the document first would move every one of them onto the relay below.

const (
	// auraClientMetadataURL is Aura's metadata document, and so its client_id. One document
	// serves every install. Published from github.com/chetto1983/aura-connect.
	auraClientMetadataURL = "https://chetto1983.github.io/aura-connect/mcp/client-metadata.json"

	// auraOAuthRelayURL is the document's only redirect URI. An authorization server must
	// match a redirect exactly, and an install's own callback is neither known in advance
	// nor https on a LAN, so the server redirects to this fixed page, which forwards the
	// browser to the callback named in `state` (relayedFetcher). It accepts only the
	// cockpit's callback path and the CLI's loopback one, https or loopback http.
	auraOAuthRelayURL = "https://chetto1983.github.io/aura-connect/mcp/callback/"
)

func useAuraClientMetadata(cfg *auth.AuthorizationCodeHandlerConfig) {
	cfg.ClientIDMetadataDocumentConfig = &auth.ClientIDMetadataDocumentConfig{URL: auraClientMetadataURL}
}

// relayedFetcher routes the authorization server's answer through auraOAuthRelayURL and on
// to target, the redirect the caller actually listens on.
//
// The target rides in `state` as `<state>.<base64url(target)>`, the shape the aura-connect
// relays decode. The SDK generated the state and compares it on return
// (authorization_code.go:594), so the suffix is added on the way out and removed on the way
// back; the fetcher it wraps never needs to know, because the callback it receives carries
// the packed state it published. A state that did not come back packed is passed through
// untouched, and the SDK's own comparison refuses it.
func relayedFetcher(target string, inner auth.AuthorizationCodeFetcher) auth.AuthorizationCodeFetcher {
	suffix := "." + base64.RawURLEncoding.EncodeToString([]byte(target))
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		parsed, err := url.Parse(args.URL)
		if err != nil {
			return nil, fmt.Errorf("mcp oauth: unparseable authorization URL: %w", err)
		}
		query := parsed.Query()
		state := query.Get("state")
		query.Set("state", state+suffix)
		parsed.RawQuery = query.Encode()

		result, err := inner(ctx, &auth.AuthorizationArgs{URL: parsed.String()})
		if err != nil || result == nil {
			return result, err
		}
		relayed := *result
		if relayed.State == state+suffix {
			relayed.State = state
		}
		return &relayed, nil
	}
}

// registrationFallback asks the primary handler — the operator's registration, or dynamic
// registration — and turns to the metadata-document handler only when the primary reports
// that the authorization server offers no way to register.
//
// The choice cannot be made up front: the redirect URI is fixed when a handler is built,
// and which registration a server accepts is learnt only from the discovery the SDK runs
// inside Authorize. So both handlers exist, and the answer to the first 401 picks one.
//
// Once the fallback has signed in it keeps every later Authorize. The primary would
// answer a plain 403 by asking for a retry without running a flow, and a session switched
// back to it would retry with no token at all.
type registrationFallback struct {
	primary  auth.OAuthHandler
	fallback auth.OAuthHandler

	mu     sync.Mutex
	active auth.OAuthHandler
}

func newRegistrationFallback(primary, fallback auth.OAuthHandler) *registrationFallback {
	return &registrationFallback{primary: primary, fallback: fallback, active: primary}
}

func (h *registrationFallback) current() auth.OAuthHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

func (h *registrationFallback) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return h.current().TokenSource(ctx)
}

// Authorize hands the same failed response to the fallback after the primary has closed
// its body. That is safe: Authorize reads only the WWW-Authenticate header, and the SDK
// documents the body as already consumed when Authorize is called.
func (h *registrationFallback) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if h.current() == h.fallback {
		return h.fallback.Authorize(ctx, req, resp)
	}
	err := h.primary.Authorize(ctx, req, resp)
	if !isNoRegistrationMethod(err) {
		return err
	}
	if err := h.fallback.Authorize(ctx, req, resp); err != nil {
		return err
	}
	h.mu.Lock()
	h.active = h.fallback
	h.mu.Unlock()
	return nil
}
