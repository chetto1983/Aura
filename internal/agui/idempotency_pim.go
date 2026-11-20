package agui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// idempotency_pim.go keeps mailbox passwords out of the idempotency ledger (amendment A9). The
// fingerprint of a PIM account create or provider-config PATCH is a plain SHA-256 of the request,
// so a password inside it could be recovered from Postgres by guessing. Each password is replaced
// by its HMAC under a key derived from the server secret before the intent is fingerprinted; the
// handler still reads the real body.

var errNoIdempotencySecretKey = errors.New("no key to hide a secret field")

// pimSecretMutationMeta is httpMutationMeta for a route whose body carries a mailbox password.
func pimSecretMutationMeta(normalizer string) mutationRouteMeta {
	meta := httpMutationMeta(normalizer)
	meta.HidesPIMSecrets = true
	return meta
}

// normalizePIMSecretMutation is normalizeHTTPMutation with each providerConfig value whose key is
// "password", in any case, replaced by base64(HMAC-SHA256(key, value)). Both names fold case
// because the sidecar reads them case-insensitively. Without a key it fails: an empty HMAC key is
// a plain hash, which A9 forbids.
func normalizePIMSecretMutation(key []byte) func(*http.Request, mutationRouteMeta) (httpMutationIntent, error) {
	return func(r *http.Request, meta mutationRouteMeta) (httpMutationIntent, error) {
		if len(key) == 0 {
			return httpMutationIntent{}, errNoIdempotencySecretKey
		}
		intent, err := normalizeHTTPMutation(r, meta)
		if err != nil || len(intent.Body) == 0 {
			return intent, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(intent.Body, &fields); err != nil {
			return httpMutationIntent{}, errors.New("mutation body is not a JSON object")
		}
		for name, raw := range fields {
			if !strings.EqualFold(name, "providerConfig") {
				continue
			}
			hidden, err := hidePasswords(key, raw)
			if err != nil {
				return httpMutationIntent{}, err
			}
			fields[name] = hidden
		}
		intent.Body, err = json.Marshal(fields)
		return intent, err
	}
}

func hidePasswords(key []byte, raw json.RawMessage) (json.RawMessage, error) {
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, errors.New("providerConfig is not a JSON object")
	}
	for name, value := range config {
		if !strings.EqualFold(name, "password") {
			continue
		}
		var password string
		if err := json.Unmarshal(value, &password); err != nil {
			return nil, errors.New("password is not a string")
		}
		hidden, err := json.Marshal(HashLookupToken(password, key))
		if err != nil {
			return nil, err
		}
		config[name] = hidden
	}
	return json.Marshal(config)
}
