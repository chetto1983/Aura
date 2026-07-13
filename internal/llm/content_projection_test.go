package llm

import (
	"context"
	"errors"
	"testing"
)

type projectionLoader struct {
	part VerifiedContentPart
	err  error
}

func (l projectionLoader) LoadContentPart(context.Context, string, string, string) (VerifiedContentPart, error) {
	return l.part, l.err
}

func TestContentProjectionSupportedAndReferenceOnly(t *testing.T) {
	part := VerifiedContentPart{ID: "p1", MIMEType: "image/png", Bytes: []byte("png"), FallbackText: "attached image"}
	got, err := ProjectContentPart(context.Background(), projectionLoader{part: part}, ProjectionPrincipal{TenantID: "t", OwnerID: "o"}, "p1", ProviderContentCapabilities{MIMETypes: map[string]bool{"image/png": true}})
	if err != nil || string(got.Bytes) != "png" || got.ReferenceOnly {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	got, err = ProjectContentPart(context.Background(), projectionLoader{part: part}, ProjectionPrincipal{TenantID: "t", OwnerID: "o"}, "p1", ProviderContentCapabilities{})
	if err != nil || !got.ReferenceOnly || got.Text != "attached image" || len(got.Bytes) != 0 {
		t.Fatalf("fallback=%+v err=%v", got, err)
	}
}

func TestContentProjectionMandatoryUnavailable(t *testing.T) {
	_, err := ProjectContentPart(context.Background(), projectionLoader{err: errors.New("unavailable")}, ProjectionPrincipal{}, "p", ProviderContentCapabilities{}, ProjectionRequirement{Mandatory: true})
	if !errors.Is(err, ErrMandatoryModalityUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
