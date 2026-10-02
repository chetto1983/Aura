package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"time"

	"github.com/chetto1983/aura/internal/webauth"
)

type credentialProvider interface {
	Handler() http.Handler
}

type bootstrapAvailabilityProvider interface {
	OperatorUserID(context.Context) (string, error)
}

func credentialProviderConfigured(provider credentialProvider) bool {
	if provider == nil {
		return false
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

type frontendAuthConfig struct {
	Provider           string `json:"provider"`
	AuthBasePath       string `json:"auth_base_path,omitempty"`
	CSRFCookieName     string `json:"csrf_cookie_name,omitempty"`
	CSRFHeaderName     string `json:"csrf_header_name,omitempty"`
	CSRFToken          string `json:"csrf_token,omitempty"`
	BootstrapAvailable bool   `json:"bootstrap_available"`
}

func newAuthConfigHandler(bootstrapProvider bootstrapAvailabilityProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		token, err := csrfTokenFor(r)
		if err != nil {
			http.Error(w, "csrf token", http.StatusInternalServerError)
			return
		}
		cfg := frontendAuthConfig{
			Provider:           "authula",
			AuthBasePath:       authBasePath,
			CSRFCookieName:     webauth.CSRFCookieName,
			CSRFHeaderName:     webauth.CSRFHeaderName,
			CSRFToken:          token,
			BootstrapAvailable: bootstrapAvailable(r.Context(), bootstrapProvider),
		}
		w.Header().Set(webauth.CSRFHeaderName, token)
		http.SetCookie(w, &http.Cookie{
			Name:     webauth.CSRFCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   int((24 * time.Hour).Seconds()),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		if err := json.NewEncoder(w).Encode(cfg); err != nil {
			http.Error(w, "auth config", http.StatusInternalServerError)
		}
	}
}

func bootstrapAvailable(ctx context.Context, provider bootstrapAvailabilityProvider) bool {
	if !bootstrapAvailabilityProviderConfigured(provider) {
		return false
	}
	operatorID, err := provider.OperatorUserID(ctx)
	if err != nil {
		return false
	}
	return operatorID == ""
}

func bootstrapAvailabilityProviderConfigured(provider bootstrapAvailabilityProvider) bool {
	if provider == nil {
		return false
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

const csrfTokenBytes = 32

// csrfTokenFor keeps the token the browser already holds when it has this handler's shape,
// and mints one otherwise. Authula compares the CSRF header with the cookie, and the browser
// keeps only the newest cookie, so a token minted on every call would fail the first of two
// login tabs. Keeping it is what Authula's own CSRF plugin does (generateCSRFTokenHook); the
// __Host- prefix stops another origin or a subdomain from planting the cookie.
func csrfTokenFor(r *http.Request) (string, error) {
	if c, err := r.Cookie(webauth.CSRFCookieName); err == nil {
		if raw, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil && len(raw) == csrfTokenBytes {
			return c.Value, nil
		}
	}
	var raw [csrfTokenBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
