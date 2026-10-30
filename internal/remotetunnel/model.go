package remotetunnel

import "time"

// Phase matches the migration's constrained reconciliation states.
type Phase string

// Persisted phases must remain in sync with the migration CHECK constraint.
const (
	PhaseDisabled           Phase = "disabled"
	PhaseValidating         Phase = "validating"
	PhaseWaitingNameservers Phase = "waiting_nameservers"
	PhaseProvisioning       Phase = "provisioning"
	PhaseConnecting         Phase = "connecting"
	PhaseHealthy            Phase = "healthy"
	PhaseDegraded           Phase = "degraded"
	PhaseError              Phase = "error"
	PhaseDeleting           Phase = "deleting"
)

// Reason is the code a failed or waiting reconciliation leaves on the row. It is never
// upstream text: the cause stays in the logs. Persisted codes must remain in sync with the
// migration CHECK constraint.
type Reason string

// "" means no reason.
const (
	ReasonTokenRejected         Reason = "token_rejected"
	ReasonNoAccounts            Reason = "no_accounts"
	ReasonAccountNotVisible     Reason = "account_not_visible"
	ReasonDomainInvalid         Reason = "domain_invalid"
	ReasonDomainElsewhere       Reason = "domain_elsewhere"
	ReasonHostnameTaken         Reason = "hostname_taken"
	ReasonNotOwned              Reason = "not_owned"
	ReasonNoAdministrator       Reason = "no_administrator"
	ReasonCloudflareUnreachable Reason = "cloudflare_unreachable"
	ReasonTunnelDown            Reason = "tunnel_down"
	ReasonOldAddressPresent     Reason = "old_address_present"
	ReasonUnknown               Reason = "unknown"
)

// AbandonedResource names something a replaced token can no longer reach in Cloudflare.
type AbandonedResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Desired carries operator intent independently of observed resources.
type Desired struct {
	Enabled                          bool
	AccountID, ZoneName, PublicLabel string
}

// Resources retains IDs across restarts; names alone never establish ownership.
type Resources struct {
	AccountID, ZoneID, TunnelID, PublicDNSID, WARPDNSID                                   string
	OTPProviderID, PublicAppID, PublicPolicyID, WARPAppID, WARPPolicyID, GatewayPostureID string
	// LegacyWARPHostname is the second hostname older installs published; retirement clears it.
	LegacyWARPHostname string
}

// State separates desired intent from the last reconciled observation.
type State struct {
	Desired       Desired
	Generation    int64
	Phase         Phase
	Resources     Resources
	TunnelName    string
	OwnedHostname string
	// OwnedZoneName is the zone the resources were created in; Desired.ZoneName may differ.
	OwnedZoneName    string
	AcceptedHostname string
	ObservedHealthy  bool
	Reason           Reason
	Abandoned        []AbandonedResource
	LastReconciledAt *time.Time
	UpdatedAt        time.Time
	UpdatedBy        string
}
