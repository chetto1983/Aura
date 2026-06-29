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

		token, err := newCSRFToken()
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

func newCSRFToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
