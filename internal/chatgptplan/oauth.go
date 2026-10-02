package chatgptplan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"golang.org/x/oauth2"
)

type endpoints struct{ issuer, authorize, token, jwks, discovery string }

func productionEndpoints() endpoints {
	return endpoints{issuer: issuer, authorize: issuer + "/api/accounts/authorize", token: issuer + "/api/accounts/oauth/token", jwks: issuer + "/.well-known/jwks.json", discovery: issuer + "/.well-known/openid-configuration"}
}

func (s *Service) oauthConfig(clientID, redirectURI string) oauth2.Config {
	return oauth2.Config{ClientID: clientID, RedirectURL: redirectURI, Scopes: strings.Fields("openid profile email offline_access resource.invoke " + planScope), Endpoint: oauth2.Endpoint{AuthURL: s.endpoint.authorize, TokenURL: s.endpoint.token, AuthStyle: oauth2.AuthStyleInParams}}
}

// Callback binds loopback delivery by one-use state and requires scoped sessions to match its owner.
func (s *Service) Callback(ctx context.Context, query url.Values) error {
	s.mu.Lock()
	s.expireFlows()
	var p *pending
	if state := query.Get("state"); state != "" && len(query["state"]) == 1 {
		for _, candidate := range s.flows {
			if candidate.state == state && candidate.flow.Status == "authorization_required" {
				p = candidate
				break
			}
		}
	}
	if p == nil || checkOwner(ctx, p.owner) != nil {
		s.mu.Unlock()
		return ErrInvalidCallback
	}
	p.state, p.flow.AuthURL, p.flow.Status = "", "", "starting"
	attempt := *p
	s.mu.Unlock()
	c, err := s.complete(ctx, &attempt, query)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flows[p.owner] != p || !p.expires.After(s.now()) || ctx.Err() != nil {
		if s.flows[p.owner] == p && ctx.Err() != nil {
			delete(s.flows, p.owner)
		}
		return ErrInvalidCallback
	}
	p.clientID = attempt.clientID
	if err == nil {
		var release func()
		release, err = s.lockStore(ctx)
		if err == nil {
			if err = ctx.Err(); err == nil {
				err = s.save(c)
			}
			release()
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			delete(s.flows, p.owner)
			return err
		}
		p.flow.Status, p.err = "error", err.Error()
		return err
	}
	p.flow.Status = "approved"
	return nil
}

func (s *Service) complete(ctx context.Context, p *pending, query url.Values) (*credential, error) {
	for _, key := range []string{"code", "client_id", "error", "iss"} {
		if len(query[key]) > 1 {
			return nil, ErrInvalidCallback
		}
	}
	if query.Get("error") != "" {
		return nil, errors.New("chatgpt plan: authorization was declined; try signing in again")
	}
	if callbackIssuer := query.Get("iss"); callbackIssuer != "" && callbackIssuer != s.endpoint.issuer {
		return nil, ErrInvalidCallback
	}
	clientID := query.Get("client_id")
	if p.clientID == dynamicClient {
		if clientID == "" || clientID == dynamicClient {
			return nil, ErrInvalidCallback
		}
		p.clientID = clientID
	} else if clientID != "" && clientID != p.clientID {
		return nil, ErrInvalidCallback
	}
	if query.Get("code") == "" {
		return nil, ErrInvalidCallback
	}
	cfg := s.oauthConfig(p.clientID, p.redirectURI)
	token, err := cfg.Exchange(s.tokenContext(ctx), query.Get("code"), oauth2.VerifierOption(p.verifier))
	if err != nil {
		return nil, errors.New("chatgpt plan: authorization exchange failed; try signing in again")
	}
	if err = validToken(token); err != nil {
		return nil, err
	}
	if scope, _ := token.Extra("scope").(string); slices.Contains(strings.Fields(scope), "offline_access") && token.RefreshToken == "" {
		return nil, errors.New("chatgpt plan: offline access grant omitted its refresh token")
	}
	idToken, _ := token.Extra("id_token").(string)
	sub, email, err := s.verifyID(ctx, idToken, p.clientID, p.nonce)
	if err != nil {
		return nil, err
	}
	if p.subject != "" && p.subject != sub {
		return nil, errors.New("chatgpt plan: signed-in account differs from the selected account")
	}
	c := &credential{Owner: p.owner, Issuer: s.endpoint.issuer, Subject: sub, ClientID: p.clientID, HostID: s.hostID, Email: email, IDToken: idToken}
	updateCredential(c, token, false)
	return c, nil
}

func (s *Service) verifyID(ctx context.Context, raw, clientID, nonce string) (string, string, error) {
	keys, err := jwk.Fetch(ctx, s.endpoint.jwks, jwk.WithHTTPClient(s.client))
	if err != nil {
		return "", "", errors.New("chatgpt plan: could not retrieve identity verification keys")
	}
	// OpenAI's OIDC discovery pins RS256. Never infer an algorithm from untrusted JWT headers.
	// https://auth.openai.com/.well-known/openid-configuration
	trustedKeys := jwk.NewSet()
	for i := range keys.Len() {
		key, ok := keys.Key(i)
		if !ok {
			continue
		}
		if key.KeyType() != jwa.RSA() {
			continue
		}
		if err = key.Set(jwk.AlgorithmKey, jwa.RS256()); err != nil {
			return "", "", errors.New("chatgpt plan: invalid identity verification key")
		}
		if err = trustedKeys.AddKey(key); err != nil {
			return "", "", errors.New("chatgpt plan: invalid identity verification key")
		}
	}
	token, err := jwt.ParseString(raw, jwt.WithKeySet(trustedKeys), jwt.WithValidate(false))
	if err != nil {
		return "", "", errors.New("chatgpt plan: invalid ID token signature")
	}
	checks := []jwt.ValidateOption{jwt.WithIssuer(s.endpoint.issuer), jwt.WithAudience(clientID), jwt.WithRequiredClaim(jwt.ExpirationKey), jwt.WithRequiredClaim(jwt.SubjectKey), jwt.WithClock(jwt.ClockFunc(s.now))}
	if nonce != "" {
		checks = append(checks, jwt.WithClaimValue("nonce", nonce))
	}
	err = jwt.Validate(token, checks...)
	if err != nil {
		return "", "", errors.New("chatgpt plan: invalid ID token identity, nonce, or expiry")
	}
	sub, _ := token.Subject()
	if sub == "" {
		return "", "", errors.New("chatgpt plan: ID token subject is empty")
	}
	var email string
	if token.Has("email") {
		if err = token.Get("email", &email); err != nil {
			return "", "", errors.New("chatgpt plan: invalid ID token email")
		}
	}
	return sub, email, nil
}

func validToken(token *oauth2.Token) error {
	if token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.Expiry.IsZero() || !token.Expiry.After(time.Now()) {
		return errors.New("chatgpt plan: invalid token response")
	}
	return nil
}

func updateCredential(c *credential, token *oauth2.Token, refresh bool) {
	c.AccessToken, c.RefreshToken, c.TokenType, c.ExpiresAt = token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry
	if scope, ok := token.Extra("scope").(string); ok {
		c.Scopes = strings.Fields(scope)
	} else if !refresh {
		c.Scopes = nil
	}
	if idToken, ok := token.Extra("id_token").(string); ok && idToken != "" {
		c.IDToken = idToken
	}
	c.EarliestRefreshAt = refreshTime(token.Extra("earliest_refresh_at"))
}

func refreshTime(value any) time.Time {
	if number, ok := value.(float64); ok && number > 0 {
		return time.Unix(int64(number), 0)
	}
	if raw, ok := value.(string); ok {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
			return time.Unix(seconds, 0)
		}
		if stamp, err := time.Parse(time.RFC3339, raw); err == nil {
			return stamp
		}
	}
	return time.Time{}
}

func (s *Service) refresh(ctx context.Context, c *credential) error {
	if c.RefreshToken == "" {
		return ErrAuthorizationRequired
	}
	if c.EarliestRefreshAt.After(s.now()) {
		if c.ExpiresAt.After(s.now()) {
			return nil
		}
		return errors.New("chatgpt plan: token refresh is not yet permitted")
	}
	cfg := s.oauthConfig(c.ClientID, "")
	previous := &oauth2.Token{AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, TokenType: c.TokenType, Expiry: time.Now().Add(-time.Minute)}
	token, err := cfg.TokenSource(s.tokenContext(ctx), previous).Token()
	if err != nil {
		var denied *oauth2.RetrieveError
		if errors.As(err, &denied) && terminalRefreshError(denied.ErrorCode) {
			c.AccessToken, c.RefreshToken, c.IDToken = "", "", ""
			delete(s.flows, c.Owner)
			if err = s.save(c); err != nil {
				return err
			}
			return ErrAuthorizationRequired
		}
		return errors.New("chatgpt plan: token refresh failed; try again")
	}
	if err = validToken(token); err != nil {
		return err
	}
	if replacement, _ := token.Extra("refresh_token").(string); replacement == "" {
		return errors.New("chatgpt plan: token refresh omitted its replacement refresh token")
	}
	if raw, _ := token.Extra("id_token").(string); raw != "" {
		sub, email, verifyErr := s.verifyID(ctx, raw, c.ClientID, "")
		if verifyErr != nil {
			return verifyErr
		}
		if sub != c.Subject {
			return errors.New("chatgpt plan: refreshed identity differs from the selected account")
		}
		if email != "" {
			c.Email = email
		}
	}
	updateCredential(c, token, true)
	if err = s.save(c); err != nil {
		return err
	}
	if !slices.Contains(c.Scopes, planScope) {
		return ErrPlanDisabled
	}
	return nil
}

// Only terminal refresh codes invalidate stored credentials; temporary service
// failures must preserve the renewable session for a later retry.
// https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery
func terminalRefreshError(code string) bool {
	switch code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	default:
		return false
	}
}

type resourceTransport struct {
	base     http.RoundTripper
	tokenURL string
}

// x/oauth2's refresh form has no option for RFC 8707 resource; its existing
// refresh/rotation implementation remains authoritative for all other fields.
// https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions
func (t resourceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.String() != t.tokenURL || request.Method != http.MethodPost {
		return t.base.RoundTrip(request)
	}
	data, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err = request.Body.Close(); err != nil {
		return nil, err
	}
	form, err := url.ParseQuery(string(data))
	if err != nil {
		return nil, err
	}
	form.Set("resource", resource)
	copy := request.Clone(request.Context())
	encoded := form.Encode()
	copy.Body = io.NopCloser(strings.NewReader(encoded))
	copy.ContentLength = int64(len(encoded))
	return t.base.RoundTrip(copy)
}

func (s *Service) tokenContext(ctx context.Context) context.Context {
	client := *s.client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = resourceTransport{base: base, tokenURL: s.endpoint.token}
	return context.WithValue(ctx, oauth2.HTTPClient, &client)
}

// Disconnect clears local tokens even when revocation fails, retaining the client mapping.
func (s *Service) Disconnect(ctx context.Context, owner string) error {
	if err := checkOwner(ctx, owner); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flows, owner)
	release, err := s.lockStore(ctx)
	if err != nil {
		return err
	}
	defer release()
	c, err := s.load(owner)
	if err != nil || c == nil {
		return err
	}
	confirmed := c.RefreshToken == "" || s.revoke(ctx, c) == nil
	c.AccessToken, c.RefreshToken, c.IDToken, c.TokenType = "", "", "", ""
	c.Scopes, c.ExpiresAt, c.EarliestRefreshAt = nil, time.Time{}, time.Time{}
	if err = s.save(c); err != nil {
		return err
	}
	if !confirmed {
		return ErrRevocationUnconfirmed
	}
	return nil
}

func (s *Service) revoke(ctx context.Context, c *credential) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint.discovery, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	var metadata struct {
		Issuer             string `json:"issuer"`
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&metadata) != nil || metadata.Issuer != s.endpoint.issuer {
		return ErrRevocationUnconfirmed
	}
	target, err := url.Parse(metadata.RevocationEndpoint)
	trusted, _ := url.Parse(s.endpoint.issuer)
	if err != nil || target.Scheme != trusted.Scheme || target.Host != trusted.Host || target.User != nil || target.Fragment != "" {
		return ErrRevocationUnconfirmed
	}
	form := url.Values{"token": {c.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {c.ClientID}}
	for attempt := range 2 {
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err = s.client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			if response.StatusCode < 500 {
				return ErrRevocationUnconfirmed
			}
		}
		if attempt == 0 {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return ErrRevocationUnconfirmed
}
