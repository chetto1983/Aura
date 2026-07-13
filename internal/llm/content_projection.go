package llm

import (
	"context"
	"errors"
	"fmt"
)

// ErrMandatoryModalityUnavailable refuses a request that cannot preserve required media.
var ErrMandatoryModalityUnavailable = errors.New("mandatory modality unavailable")

// ProjectionPrincipal carries the tenant and owner authorization scope.
type ProjectionPrincipal struct{ TenantID, OwnerID string }

// VerifiedContentPart is authorized, digest-checked media ready for projection.
type VerifiedContentPart struct {
	ID, MIMEType, Digest, FallbackText string
	Bytes                              []byte
}

// ContentPartLoader reloads and verifies content immediately before request construction.
type ContentPartLoader interface {
	LoadContentPart(context.Context, string, string, string) (VerifiedContentPart, error)
}

// ProviderContentCapabilities declares accepted MIME types.
type ProviderContentCapabilities struct{ MIMETypes map[string]bool }

// ProjectionRequirement controls whether reference-only fallback is permitted.
type ProjectionRequirement struct{ Mandatory bool }

// ProjectedRequestPart is the provider-neutral outbound media representation.
type ProjectedRequestPart struct {
	Type, MIMEType, ReferenceID, Text string
	Bytes                             []byte
	ReferenceOnly                     bool
}

// ProjectContentPart emits verified bytes or an explicit stored-text reference fallback.
func ProjectContentPart(ctx context.Context, loader ContentPartLoader, principal ProjectionPrincipal, id string, caps ProviderContentCapabilities, requirements ...ProjectionRequirement) (ProjectedRequestPart, error) {
	part, err := loader.LoadContentPart(ctx, principal.TenantID, principal.OwnerID, id)
	mandatory := len(requirements) > 0 && requirements[0].Mandatory
	if err != nil {
		if mandatory {
			return ProjectedRequestPart{}, fmt.Errorf("%w: %v", ErrMandatoryModalityUnavailable, err)
		}
		return ProjectedRequestPart{}, err
	}
	if caps.MIMETypes[part.MIMEType] {
		return ProjectedRequestPart{Type: "media", MIMEType: part.MIMEType, ReferenceID: part.ID, Bytes: append([]byte(nil), part.Bytes...)}, nil
	}
	if mandatory {
		return ProjectedRequestPart{}, ErrMandatoryModalityUnavailable
	}
	return ProjectedRequestPart{Type: "reference", MIMEType: part.MIMEType, ReferenceID: part.ID, Text: part.FallbackText, ReferenceOnly: true}, nil
}
