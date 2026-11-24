package agui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// idempotency_pim_mutation_test.go pins the inputs normalizePIMSecretMutation refuses or passes
// through untouched: the ledger must never see a body it could not hide the password of.

func TestThePIMNormalizerRefusesWhatItCannotHide(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	for _, tc := range []struct{ name, body, want string }{
		{"not JSON", `{"providerConfig":`, "mutation body is not valid JSON"},
		{"a single-byte body", `5`, "mutation body is not a JSON object"},
		{"a JSON array", `[1,2]`, "mutation body is not a JSON object"},
		{"providerConfig is a string", `{"providerConfig":"imap.example.com"}`, "providerConfig is not a JSON object"},
		{"password is a number", `{"providerConfig":{"password":123}}`, "password is not a string"},
		{"password is an object", `{"providerConfig":{"Password":{"v":"x"}}}`, "password is not a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/connect/pim/accounts", strings.NewReader(tc.body))
			meta := httpMutationRoutes["POST /api/connect/pim/accounts"]

			intent, err := normalizePIMSecretMutation(key)(req, meta)

			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if len(intent.Body) != 0 {
				t.Fatalf("a refused body still reached the intent: %s", intent.Body)
			}
		})
	}
}

func TestThePIMNormalizerPassesAnEmptyBodyThrough(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	req := httptest.NewRequest(http.MethodPatch, pimPatchPath, nil)

	intent, err := normalizePIMSecretMutation(key)(req, httpMutationRoutes["PATCH /api/connect/pim/accounts/{id}/provider-config"])

	if err != nil || len(intent.Body) != 0 || intent.Method != http.MethodPatch {
		t.Fatalf("empty body = %+v, %v; want the intent unchanged and no error", intent, err)
	}
}

func TestThePIMNormalizerLeavesOtherFieldsAlone(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	body := `{"providerConfig":{"imapPort":993,"username":"someone@example.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/connect/pim/accounts", strings.NewReader(body))

	intent, err := normalizePIMSecretMutation(key)(req, httpMutationRoutes["POST /api/connect/pim/accounts"])

	if err != nil || string(intent.Body) != body {
		t.Fatalf("intent body = %s, %v; want the fields untouched", intent.Body, err)
	}
}
