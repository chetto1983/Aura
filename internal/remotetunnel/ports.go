package remotetunnel

import (
	"context"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// StateStore preserves intent generations and independently committed resource IDs.
type StateStore interface {
	Load(context.Context) (State, error)
	SaveDesired(context.Context, int64, Desired, string) (State, error)
	Advance(context.Context, State) (State, error)
}

// Members supplies active human identity emails, never account-wide domains.
type Members interface {
	ActiveEmails(context.Context) ([]string, error)
}

// Administrators must be active human identities holding identity.create.
type Administrators interface {
	ActiveAdminEmails(context.Context) ([]string, error)
}

// ProjectionState excludes its credential from serialization.
type ProjectionState struct {
	Enabled    bool                 `json:"enabled"`
	Generation int64                `json:"generation"`
	Token      cloudflareapi.Secret `json:"-"`
}

// Projection applies disposable sidecar state derived from PostgreSQL.
type Projection interface {
	Apply(context.Context, ProjectionState) error
}

// TunnelCredentials must commit encrypted PostgreSQL state before Save returns success.
type TunnelCredentials interface {
	Save(context.Context, cloudflareapi.Secret) error
}

// Acceptance includes supervisor readiness and authenticated external acceptance.
type Acceptance interface {
	Ready(context.Context, State) (bool, error)
}

// Cloudflare is the control-plane surface consumed by the resumable service.
type Cloudflare interface {
	VerifyToken(context.Context) (cloudflareapi.TokenVerification, error)
	ListAccounts(context.Context) ([]cloudflareapi.Account, error)
	ListZones(context.Context, string, string) ([]cloudflareapi.Zone, error)
	GetZone(context.Context, string) (cloudflareapi.Zone, error)
	CreateZone(context.Context, string, string) (cloudflareapi.Zone, error)
	ListTunnels(context.Context, string) ([]cloudflareapi.Tunnel, error)
	GetTunnel(context.Context, string, string) (cloudflareapi.Tunnel, error)
	CreateTunnel(context.Context, string, string) (cloudflareapi.Tunnel, error)
	DeleteTunnel(context.Context, string, string) error
	TunnelToken(context.Context, string, string) (cloudflareapi.Secret, error)
	PutTunnelConfig(context.Context, string, string, cloudflareapi.TunnelConfig) error
	ListIdentityProviders(context.Context, string) ([]cloudflareapi.IdentityProvider, error)
	EnsureOTPProvider(context.Context, string) (cloudflareapi.IdentityProvider, error)
	ListApplications(context.Context, string) ([]cloudflareapi.AccessApplication, error)
	GetApplication(context.Context, string, string) (cloudflareapi.AccessApplication, error)
	PutApplication(context.Context, string, string, cloudflareapi.AccessApplication) (cloudflareapi.AccessApplication, error)
	DeleteApplication(context.Context, string, string) error
	ListPolicies(context.Context, string, string) ([]cloudflareapi.AccessPolicy, error)
	GetPolicy(context.Context, string, string, string) (cloudflareapi.AccessPolicy, error)
	PutPolicy(context.Context, string, string, string, cloudflareapi.AccessPolicy) (cloudflareapi.AccessPolicy, error)
	DeletePolicy(context.Context, string, string, string) error
	ListPosture(context.Context, string) ([]cloudflareapi.Posture, error)
	GetPosture(context.Context, string, string) (cloudflareapi.Posture, error)
	EnsureGatewayPosture(context.Context, string, string, string, string) (cloudflareapi.Posture, error)
	DeletePosture(context.Context, string, string, string) error
	ListDNS(context.Context, string, string) ([]cloudflareapi.DNSRecord, error)
	GetDNS(context.Context, string, string) (cloudflareapi.DNSRecord, error)
	EnsureCNAME(context.Context, string, string, string, string, string) (cloudflareapi.DNSRecord, error)
	DeleteDNS(context.Context, string, string, string) error
}

var _ Cloudflare = (*cloudflareapi.Client)(nil)
