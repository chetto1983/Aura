package garageadmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
