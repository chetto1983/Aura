package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var (
	// ErrContentPartUnavailable intentionally hides missing versus unauthorized parts.
	ErrContentPartUnavailable = errors.New("content part unavailable")
	// ErrContentPartCorrupt reports a verified digest or length mismatch.
	ErrContentPartCorrupt = errors.New("content part corrupt")
	// ErrContentPartMigrated reports a part whose storage location has moved.
	ErrContentPartMigrated = errors.New("content part migrated")
)

// RetentionClass controls content-part expiry policy.
type RetentionClass string

// RetentionDurable retains bytes while any reachability edge exists.
const RetentionDurable RetentionClass = "durable"

// ReachabilityKind identifies a durable reference source.
type ReachabilityKind string

// Supported reachability edge kinds.
const (
	ReachabilityTurn       ReachabilityKind = "turn"
	ReachabilityCheckpoint ReachabilityKind = "checkpoint"
	ReachabilityMemory     ReachabilityKind = "memory"
)

// PartPrincipal is the authorization scope rechecked on every reload.
type PartPrincipal struct{ TenantID, OwnerID string }

// PutContentPart describes immutable bytes and their lifecycle metadata.
type PutContentPart struct {
	TenantID, OwnerID, MIMEType, EncryptionClass, FallbackText string
	Bytes                                                      []byte
	Retention                                                  RetentionClass
	ProviderRequirements                                       []string
	ExpiresAt                                                  time.Time
}

// ContentPart is a digest-verified immutable typed payload.
type ContentPart struct {
	ID, StorageID, TenantID, OwnerID, MIMEType, Digest, EncryptionClass, FallbackText string
	ByteLength                                                                        int64
	Bytes                                                                             []byte
	Retention                                                                         RetentionClass
	ProviderRequirements                                                              []string
	ExpiresAt                                                                         time.Time
}

// ContentPartLink keeps a part reachable from a canonical artifact.
type ContentPartLink struct {
	PartID, TurnID, CheckpointID, MemoryID string
	Kind                                   ReachabilityKind
}

func digestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
