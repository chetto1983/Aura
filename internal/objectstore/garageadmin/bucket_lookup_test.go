package garageadmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// bucket_lookup_test.go pins BucketIDByAlias's not-found signal. A purge running in a later
// process than the one that provisioned a bucket has only the alias to go on, and it must be
// able to tell "the bucket is already gone" (a converged step) apart from "the admin API
// answered something else" (a step that must fail and be retried). Measured 2026-09-08 on a
// live Garage: GetBucketInfo answers 404 for an unknown globalAlias.

func TestBucketIDByAliasReportsNotFound(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NoSuchBucket"}`))
	})
	_, err := c.BucketIDByAlias(context.Background(), "aura-gone")
	if !errors.Is(err, ErrBucketNotFound) {
		t.Fatalf("BucketIDByAlias(404) err = %v, want ErrBucketNotFound", err)
	}
}

func TestBucketIDByAliasNonNotFoundStaysDistinct(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"InternalError"}`))
	})
	_, err := c.BucketIDByAlias(context.Background(), "aura-x")
	if err == nil {
		t.Fatal("BucketIDByAlias(500) err = nil, want a failure")
	}
	if errors.Is(err, ErrBucketNotFound) {
		t.Fatalf("BucketIDByAlias(500) err = %v, want it NOT classified as not-found — an unreachable admin API is not an absent bucket", err)
	}
}

// KeyIDByName is the other half of the same problem. A teardown resuming after the
// credential row is gone has no access key id left to delete by — only the key NAME, which
// CreateKey derives from the identity. Measured 2026-09-08 against the live Garage admin
// API: GET /v2/GetKeyInfo?search=<name> answers 200 with the accessKeyId, and 404 when no
// key carries that name.
func TestKeyIDByNameResolvesAndReportsNotFound(t *testing.T) {
	t.Run("resolves the access key id", func(t *testing.T) {
		c, got := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"accessKeyId":"GK-live","name":"aura-x"}`))
		})
		id, err := c.KeyIDByName(context.Background(), "aura-x")
		if err != nil {
			t.Fatalf("KeyIDByName err = %v, want nil", err)
		}
		if id != "GK-live" {
			t.Fatalf("KeyIDByName id = %q, want GK-live", id)
		}
		if got.Path != "/v2/GetKeyInfo" {
			t.Fatalf("path = %q, want /v2/GetKeyInfo", got.Path)
		}
		if !strings.Contains(got.Query, "search=aura-x") {
			t.Fatalf("query = %q, want it to search by name", got.Query)
		}
	})

	t.Run("404 is ErrKeyNotFound", func(t *testing.T) {
		c, _ := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			w.WriteHeader(http.StatusNotFound)
		})
		if _, err := c.KeyIDByName(context.Background(), "aura-gone"); !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("KeyIDByName(404) err = %v, want ErrKeyNotFound", err)
		}
	})

	t.Run("an ambiguous or failing search is not not-found", func(t *testing.T) {
		c, _ := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"MultipleKeys"}`))
		})
		_, err := c.KeyIDByName(context.Background(), "aura-x")
		if err == nil || errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("KeyIDByName(400) err = %v, want a distinct failure — an ambiguous search must never read as an absent key", err)
		}
	})
}

func TestBucketIDByAliasSuccessUnchanged(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bucketInfo{ID: "bkt-1", GlobalAliases: []string{"aura-x"}})
	})
	id, err := c.BucketIDByAlias(context.Background(), "aura-x")
	if err != nil {
		t.Fatalf("BucketIDByAlias err = %v, want nil", err)
	}
	if id != "bkt-1" {
		t.Fatalf("BucketIDByAlias id = %q, want bkt-1", id)
	}
}
