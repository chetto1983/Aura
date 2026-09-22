package cloudflareapi

import (
	"fmt"
	"io"
	"time"
)

// Secret requires Reveal for plaintext; logs and serializers receive a redaction.
type Secret string

// String prevents accidental disclosure through string formatting.
func (Secret) String() string { return "[REDACTED]" }

// GoString prevents accidental disclosure through Go-syntax formatting.
func (Secret) GoString() string { return "[REDACTED]" }

// Format prevents diagnostic formatting from exposing credentials.
func (Secret) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "[REDACTED]") }

// MarshalJSON excludes credentials from API responses.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

// MarshalText excludes credentials from text serializers.
func (Secret) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }

// Reveal is the explicit boundary for credential-consuming code.
func (s Secret) Reveal() string { return string(s) }

// TokenVerification exposes status without echoing the verified credential.
type TokenVerification struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Account is the selectable account identity returned by discovery.
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Zone preserves pending activation and registrar nameserver instructions.
type Zone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Account     Account  `json:"account"`
	NameServers []string `json:"name_servers"`
}

// Tunnel omits credentials returned as extra fields during creation.
type Tunnel struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	RemoteConfig bool       `json:"remote_config"`
	ConfigSource string     `json:"config_src"`
	DeletedAt    *time.Time `json:"deleted_at"`
}

// Ingress omits hostname on the mandatory final catch-all rule.
type Ingress struct {
	Hostname string `json:"hostname,omitempty"`
	Service  string `json:"service"`
}

// TunnelConfig cannot encode private-network routes.
type TunnelConfig struct {
	Ingress []Ingress `json:"ingress"`
}

// DNSRecord retains the ownership comment needed before mutation.
type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
	Comment string `json:"comment"`
}

// IdentityProvider omits provider secrets and configuration.
type IdentityProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// AccessApplication models only the self-hosted application contract.
type AccessApplication struct {
	ID                     string   `json:"id,omitempty"`
	Name                   string   `json:"name"`
	Domain                 string   `json:"domain"`
	Type                   string   `json:"type"`
	SessionDuration        string   `json:"session_duration"`
	AllowedIDPs            []string `json:"allowed_idps"`
	AutoRedirectToIdentity bool     `json:"auto_redirect_to_identity"`
}

// EmailRule matches one address, never an entire domain.
type EmailRule struct {
	Email string `json:"email"`
}

// IDRule binds authentication to one identity provider.
type IDRule struct {
	ID string `json:"id"`
}

// PostureRule requires a successful check identified by its integration UID.
type PostureRule struct {
	IntegrationUID string `json:"integration_uid"`
}

// AccessRule deliberately cannot encode Everyone or bypass selectors.
type AccessRule struct {
	Email         *EmailRule   `json:"email,omitempty"`
	LoginMethod   *IDRule      `json:"login_method,omitempty"`
	DevicePosture *PostureRule `json:"device_posture,omitempty"`
}

// AccessPolicy separates email includes from mandatory OTP/posture requirements.
type AccessPolicy struct {
	ID         string       `json:"id,omitempty"`
	Name       string       `json:"name"`
	Decision   string       `json:"decision"`
	Precedence int          `json:"precedence"`
	Include    []AccessRule `json:"include"`
	Require    []AccessRule `json:"require"`
	Exclude    []AccessRule `json:"exclude"`
}

// Posture retains the ownership description used before removal.
type Posture struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Type string `json:"type"`
}
