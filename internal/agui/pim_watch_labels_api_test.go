package agui

import (
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/pimwatch"
)

func TestMailWatchLabelsCarryDescriptionAndDiscard(t *testing.T) {
	service := &fakeWatchService{labels: []pimwatch.Label{{ID: "l1", Name: "Cliente", Description: "Customers.", Discard: true}}}
	labels := decodeWatch[[]mailWatchLabelJSON](t, mailWatchCall(t, service, http.MethodGet, "/api/mail-watch/labels", ""))
	if len(labels) != 1 || labels[0].Description != "Customers." || !labels[0].Discard {
		t.Fatalf("labels = %+v", labels)
	}
	added := decodeWatch[mailWatchLabelJSON](t, mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/labels",
		`{"name":"Spam","tier":"later","description":"Offers sent to a list.","discard":true}`))
	want := pimwatch.LabelInput{Name: "Spam", Tier: pimwatch.TierLater, Description: "Offers sent to a list.", Discard: true}
	if service.newLabel != want || added.Description != want.Description || !added.Discard {
		t.Fatalf("added = %+v, reached the service as %+v", added, service.newLabel)
	}
}

func TestALabelPutKeepsWhatItDoesNotSend(t *testing.T) {
	service := &fakeWatchService{}
	mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/labels/l1", `{"name":"Clienti"}`)
	if service.patch.Name == nil || *service.patch.Name != "Clienti" ||
		service.patch.Tier != nil || service.patch.Description != nil || service.patch.Discard != nil {
		t.Fatalf("patch = %+v", service.patch)
	}
	mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/labels/l1", `{"tier":"","description":"","discard":false}`)
	if service.patch.Name != nil || service.patch.Tier == nil || *service.patch.Tier != "" ||
		service.patch.Description == nil || service.patch.Discard == nil || *service.patch.Discard {
		t.Fatalf("a field sent empty must reach the service as set: %+v", service.patch)
	}
}
