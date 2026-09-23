// Package pimprovider holds the OAuth client an admin sets once per PIM provider, and the rules
// that decide which providers are managed that way. Aura injects the client into every
// account-create it forwards to the aura-pim-mcp sidecar, so members never type one
// (docs/superpowers/specs/2026-09-23-pim-provider-apps-design.md).
package pimprovider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The managed provider ids, exactly as the cockpit sends them.
const (
	Google       = "google"
	Microsoft365 = "microsoft365"
	OutlookCom   = "outlook.com"
)

var (
	// ErrNotConfigured reports a managed provider with no admin-set app yet.
	ErrNotConfigured = errors.New("pimprovider: provider app not configured")
	// ErrInvalid wraps every Validate refusal; its message names the field to fix.
	ErrInvalid = errors.New("pimprovider: invalid provider app")
	// ErrStale reports a keep-secret save whose client ID is no longer the stored one: another
	// save landed between the read that validated it and the write.
	ErrStale = errors.New("pimprovider: the provider app changed; reload and save again")
)

// canonical is every id the cockpit sends. The sidecar matches provider names without regard to
// case, so anything but these exact bytes could name a managed provider while dodging injection.
var canonical = []string{Google, Microsoft365, OutlookCom, "imap", "ics", "json"}

var managed = []string{Google, Microsoft365, OutlookCom}

var ownedKeys = []string{"clientId", "clientSecret", "tenantId"}

// App is one provider's OAuth client. ClientSecret is plaintext and lives only in memory; on a
// save, an empty ClientSecret for Google means "keep the stored one".
type App struct {
	Provider     string
	ClientID     string
	TenantID     string
	ClientSecret string
	SecretSet    bool
	UpdatedAt    time.Time
	UpdatedBy    string
}

// Canonical reports whether provider is one of the ids the cockpit sends, byte for byte.
func Canonical(provider string) bool { return slices.Contains(canonical, provider) }

// Managed reports whether provider takes its OAuth client from an admin-set app.
func Managed(provider string) bool { return slices.Contains(managed, provider) }

// ManagedProviders lists the managed ids in display order.
func ManagedProviders() []string { return slices.Clone(managed) }

// IsOwnedKey ignores case because the sidecar folds providerConfig keys; a leftover "ClientId"
// beside the injected "clientId" makes its dictionary copy throw.
func IsOwnedKey(key string) bool {
	return slices.ContainsFunc(ownedKeys, func(k string) bool { return strings.EqualFold(k, key) })
}

// ProviderConfig is exactly the providerConfig the sidecar reads for this provider.
func (a App) ProviderConfig() map[string]string {
	if a.Provider == Google {
		return map[string]string{"clientId": a.ClientID, "clientSecret": a.ClientSecret}
	}
	return map[string]string{"clientId": a.ClientID, "tenantId": a.TenantID}
}

// Validate checks a save against the stored app (nil when there is none). A Google save without
// a secret is accepted only for the same client ID: a new client without its own secret would
// save fine and then fail every consent at the code exchange, where the admin never looks.
func Validate(next App, stored *App) error {
	if !Managed(next.Provider) {
		return fmt.Errorf("%w: %q is not a managed provider", ErrInvalid, next.Provider)
	}
	if next.ClientID == "" {
		return fmt.Errorf("%w: clientId is required", ErrInvalid)
	}
	if next.Provider != Google {
		if next.TenantID == "" {
			return fmt.Errorf("%w: tenantId is required for %s", ErrInvalid, next.Provider)
		}
		if next.ClientSecret != "" {
			return fmt.Errorf("%w: %s uses a public client and takes no clientSecret", ErrInvalid, next.Provider)
		}
		return nil
	}
	if next.TenantID != "" {
		return fmt.Errorf("%w: google takes no tenantId", ErrInvalid)
	}
	if next.ClientSecret == "" && (stored == nil || !stored.SecretSet || stored.ClientID != next.ClientID) {
		return fmt.Errorf("%w: clientSecret is required for a new Google client", ErrInvalid)
	}
	return nil
}
