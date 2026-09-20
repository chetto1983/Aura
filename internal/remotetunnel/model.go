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

// Desired carries operator intent independently of observed resources.
type Desired struct {
	Enabled                                     bool
	AccountID, ZoneName, PublicLabel, WARPLabel string
}

// Resources retains IDs across restarts; names alone never establish ownership.
type Resources struct {
	AccountID, ZoneID, TunnelID, PublicDNSID, WARPDNSID                                   string
	OTPProviderID, PublicAppID, PublicPolicyID, WARPAppID, WARPPolicyID, GatewayPostureID string
}

// State separates desired intent from the last reconciled observation.
type State struct {
	Desired          Desired
	Generation       int64
	Phase            Phase
	Resources        Resources
	TunnelName       string
	ObservedHealthy  bool
	LastError        string
	LastReconciledAt *time.Time
	UpdatedAt        time.Time
	UpdatedBy        string
}
