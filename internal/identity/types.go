// Package identity owns Aura's durable principal, actor, and capability
// authorization boundary.
package identity

import (
	"strings"
	"time"
)

type PrincipalKind string

const (
	PrincipalKindHuman   PrincipalKind = "human"
	PrincipalKindSystem  PrincipalKind = "system"
	PrincipalKindService PrincipalKind = "service"
	PrincipalKindOwner   PrincipalKind = "owner"
)

type PrincipalStatus string

const (
	PrincipalStatusActive   PrincipalStatus = "active"
	PrincipalStatusDisabled PrincipalStatus = "disabled"
)

type ActorType string

const (
	ActorTypeSession ActorType = "session"
	ActorTypeRun     ActorType = "run"
	ActorTypeCron    ActorType = "cron"
	ActorTypeSwarm   ActorType = "swarm"
	ActorTypeSystem  ActorType = "system"
)

type SubjectType string

const (
	SubjectTypePrincipal SubjectType = "principal"
	SubjectTypeActor     SubjectType = "actor"
)

type Capability string

const (
	CapabilityAPIChat         Capability = "api.chat"
	CapabilityDashboardRead   Capability = "dashboard.read"
	CapabilityDashboardWrite  Capability = "dashboard.write"
	CapabilityToolExecute     Capability = "tool.execute"
	CapabilitySkillsInstall   Capability = "skills.install"
	CapabilitySkillsDelete    Capability = "skills.delete"
	CapabilitySettingsWrite   Capability = "settings.write"
	CapabilityCronCreate      Capability = "cron.create"
	CapabilityCronRun         Capability = "cron.run"
	CapabilitySwarmSpawn      Capability = "swarm.spawn"
	CapabilityMemoryUserWrite Capability = "memory.user.write"
	CapabilityWikiWrite       Capability = "wiki.write"
)

func ToolExecuteCapability(toolName string) Capability {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return CapabilityToolExecute
	}
	return Capability("tool.execute." + toolName)
}

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

type ResourceRef struct {
	Type string
	ID   string
}

type Principal struct {
	ID           string
	Kind         PrincipalKind
	DisplayName  string
	Status       PrincipalStatus
	CreatedAt    time.Time
	MetadataJSON string
}

type ChannelAccount struct {
	ID           string
	PrincipalID  string
	Provider     string
	ExternalID   string
	DisplayName  string
	CreatedAt    time.Time
	LastSeenAt   *time.Time
	MetadataJSON string
}

type Actor struct {
	ID               string
	PrincipalID      string
	ActorType        ActorType
	ParentActorID    string
	ChannelAccountID string
	RunID            string
	CreatedAt        time.Time
	ExpiresAt        *time.Time
	MetadataJSON     string
}

type Grant struct {
	ID               string
	SubjectType      SubjectType
	SubjectID        string
	Capability       Capability
	Resource         ResourceRef
	ConstraintsJSON  string
	GrantedByActorID string
	CreatedAt        time.Time
	ExpiresAt        *time.Time
	RevokedAt        *time.Time
}

type AuthorizationDecision struct {
	ID         string
	ActorID    string
	Capability Capability
	Resource   ResourceRef
	Decision   Decision
	Reason     string
	RunID      string
	EventID    string
	CreatedAt  time.Time
}

type PrincipalParams struct {
	ID           string
	Kind         PrincipalKind
	DisplayName  string
	Status       PrincipalStatus
	MetadataJSON string
}

type ChannelAccountParams struct {
	ID           string
	PrincipalID  string
	Provider     string
	ExternalID   string
	DisplayName  string
	LastSeenAt   *time.Time
	MetadataJSON string
}

type ActorParams struct {
	ID               string
	PrincipalID      string
	ActorType        ActorType
	ParentActorID    string
	ChannelAccountID string
	RunID            string
	ExpiresAt        *time.Time
	MetadataJSON     string
}

type GrantParams struct {
	ID               string
	SubjectType      SubjectType
	SubjectID        string
	Capability       Capability
	Resource         ResourceRef
	ConstraintsJSON  string
	GrantedByActorID string
	ExpiresAt        *time.Time
}

type AuthorizeParams struct {
	ActorID    string
	Capability Capability
	Resource   ResourceRef
	RunID      string
	EventID    string
}

type TelegramAllowlistUserParams struct {
	UserID        string
	Source        string
	PrincipalKind PrincipalKind
	DisplayName   string
	Capabilities  []Capability
}

type TelegramAllowlistBackfillResult struct {
	PrincipalCreated      bool
	ChannelAccountCreated bool
	ActorCreated          bool
	GrantsCreated         int
}

type TelegramAllowlistBackfillSummary struct {
	Users                  int
	PrincipalsCreated      int
	ChannelAccountsCreated int
	ActorsCreated          int
	GrantsCreated          int
}

type DelegateActorParams struct {
	ID              string
	ParentActorID   string
	ActorType       ActorType
	RunID           string
	Capabilities    []Capability
	Resource        ResourceRef
	ConstraintsJSON string
	ExpiresAt       *time.Time
	MetadataJSON    string
}

type DelegateActorResult struct {
	Actor         Actor
	ActorCreated  bool
	GrantsCreated int
	Decisions     []AuthorizationDecision
}
