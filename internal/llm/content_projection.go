package llm

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
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

// ContentProjection carries only request-scoped, authorized references through the
// runner/agent layers. Bytes are deliberately absent: the provider client reloads and
// verifies them immediately before constructing the outbound request.
type ContentProjection struct {
	Loader       ContentPartLoader
	Principal    ProjectionPrincipal
	ReferenceIDs []string
}

type contentProjectionContextKey struct{}

// WithContentProjection binds an immutable copy of the explicit current-turn refs.
func WithContentProjection(ctx context.Context, projection ContentProjection) context.Context {
	projection.ReferenceIDs = append([]string(nil), projection.ReferenceIDs...)
	return context.WithValue(ctx, contentProjectionContextKey{}, projection)
}

// ContentProjectionFromContext returns a copy so hooks/consumers cannot rewrite the
// authorization set stored in the request context.
func ContentProjectionFromContext(ctx context.Context) (ContentProjection, bool) {
	projection, ok := ctx.Value(contentProjectionContextKey{}).(ContentProjection)
	if !ok || projection.Loader == nil || len(projection.ReferenceIDs) == 0 {
		return ContentProjection{}, false
	}
	projection.ReferenceIDs = append([]string(nil), projection.ReferenceIDs...)
	return projection, true
}

// ProviderContentCapabilities is the provider-advertised native input surface.
// Modalities uses Aura's normalized names (text/image/audio/file/video); MIMETypes
// remains as an exact override for adapters whose provider exposes MIME-level caps.
type ProviderContentCapabilities struct {
	Modalities map[string]bool
	MIMETypes  map[string]bool
}

// SupportsMIME maps a concrete, normalized media type onto the provider's advertised
// modality. Parameters never participate in the decision (audio/wav; codecs=... is
// still audio). Unknown and malformed types fail closed.
func (c ProviderContentCapabilities) SupportsMIME(value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil || mediaType == "" {
		return false
	}
	if c.MIMETypes[mediaType] {
		return true
	}
	major, _, ok := strings.Cut(mediaType, "/")
	return ok && c.Modalities[major]
}

// ContentCapabilitySource resolves the active model's native input surface. A false
// result is a safe text-only floor: callers retain the stored transcript/summary and
// must not guess capability from a model name.
type ContentCapabilitySource interface {
	ContentCapabilities(context.Context) (ProviderContentCapabilities, bool)
}

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
	if caps.SupportsMIME(part.MIMEType) {
		return ProjectedRequestPart{Type: "media", MIMEType: part.MIMEType, ReferenceID: part.ID, Bytes: append([]byte(nil), part.Bytes...)}, nil
	}
	if mandatory {
		return ProjectedRequestPart{}, ErrMandatoryModalityUnavailable
	}
	return ProjectedRequestPart{Type: "reference", MIMEType: part.MIMEType, ReferenceID: part.ID, Text: part.FallbackText, ReferenceOnly: true}, nil
}
