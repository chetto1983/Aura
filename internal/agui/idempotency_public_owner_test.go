package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// A public mutation runs with NO authenticated principal — that is what makes it public —
// so the middleware has to pick an owner for its idempotency key. It must pick one that
// still exists.
//
// LocalOperatorIdentity does not. serve_auth.go's retireLegacyLocalIdentityForAuthulaUser
// migrates every reference onto the first enrolled operator and DELETES the seed row, and
// idempotency_operations.identity_id is FK'd to identities. So from the first operator
// onward, every public mutation wrote a foreign key to a row that was gone, Begin failed,
// and the endpoint answered 503 for the life of the deployment.
//
// Measured live 2026-08-31: POST /api/auth/password-reset/start returned 503 three times
// to an operator locked out of their own appliance. The bootstrap route fails identically,
// which is worse: it is how the first operator is created.
func TestPublicMutationsAreOwnedByAnIdentityThatSurvives(t *testing.T) {
	t.Parallel()

	registry := &memoryHTTPRegistry{}
	server := NewServer(&scriptedRunner{newConversationID: goodID}, &fakeConvStore{known: map[string]bool{goodID: true}}, ServerConfig{})
	server.SetOperationRegistry(registry)

	// No identityctx on the request: an unauthenticated caller, as the real route is.
	r := httptest.NewRequest(http.MethodPost, "/api/auth/password-reset/start",
		strings.NewReader(`{"email":"someone@example.com"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "public-mutation-owner-1")
	server.Mux().ServeHTTP(httptest.NewRecorder(), r)

	if registry.request == nil {
		t.Fatal("the public mutation never reached the operation registry")
	}
	owner := registry.request.Operation.IdentityID
	if owner == identityctx.LocalOperatorIdentity {
		t.Fatal("public mutations must NOT be owned by the `local` seed: it is deleted the " +
			"first time an operator enrols, and the FK takes the idempotency row with it")
	}
	if owner != identityctx.CLIServiceIdentity {
		t.Fatalf("public mutation owner = %q, want the surviving service identity %q",
			owner, identityctx.CLIServiceIdentity)
	}
}
