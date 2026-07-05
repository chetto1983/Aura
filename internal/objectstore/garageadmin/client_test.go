package garageadmin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "test-admin-token"

// capturedRequest records what the client sent, for assertions.
type capturedRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   map[string]any
}

// newMock spins up an httptest server whose per-path handler is route. It records
// every request into *got (last write wins per field is fine for single-call tests;
// tests that issue two calls inspect via the route closure instead).
func newMock(t *testing.T, route func(w http.ResponseWriter, r *http.Request, body map[string]any)) (*Client, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		got.Method = r.Method
		got.Path = r.URL.Path
		got.Query = r.URL.RawQuery
		got.Auth = r.Header.Get("Authorization")
		got.Body = body
		route(w, r, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, testToken, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, got
}

func TestNewRejectsEmptyConfig(t *testing.T) {
	if _, err := New("", testToken); !errors.Is(err, ErrEmptyConfig) {
		t.Errorf("New(empty endpoint) err = %v, want ErrEmptyConfig", err)
	}
	if _, err := New("http://garage:3903", ""); !errors.Is(err, ErrEmptyConfig) {
		t.Errorf("New(empty token) err = %v, want ErrEmptyConfig", err)
	}
	if _, err := New("http://garage:3903", "tok"); err != nil {
		t.Errorf("New(valid) err = %v, want nil", err)
	}
}

func TestBucketForIdentity(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	got, err := BucketForIdentity(id)
	if err != nil {
		t.Fatalf("BucketForIdentity(%q) err = %v", id, err)
	}
	if want := "aura-" + id; got != want {
		t.Errorf("BucketForIdentity = %q, want %q", got, want)
	}
	for _, bad := range []string{"", "..", "../evil", "a/b", `a\b`, "has space", strings.Repeat("x", 100)} {
		if _, err := BucketForIdentity(bad); err == nil {
			t.Errorf("BucketForIdentity(%q) = nil error, want rejection", bad)
		}
	}
}

func TestCreateBucketReturnsID(t *testing.T) {
	c, got := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bucketInfo{ID: "bkt-1", GlobalAliases: []string{"aura-x"}})
	})
	id, err := c.CreateBucket(context.Background(), "aura-x")
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if id != "bkt-1" {
		t.Errorf("CreateBucket id = %q, want bkt-1", id)
	}
	if got.Path != "/v2/CreateBucket" || got.Method != http.MethodPost {
		t.Errorf("request = %s %s, want POST /v2/CreateBucket", got.Method, got.Path)
	}
	if got.Auth != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want bearer token", got.Auth)
	}
	if got.Body["globalAlias"] != "aura-x" {
		t.Errorf("body globalAlias = %v, want aura-x", got.Body["globalAlias"])
	}
}

// TestCreateBucketIdempotentOnConflict: a 409 already-exists resolves to the existing
// bucket's id via GetBucketInfo (idempotent saga step).
func TestCreateBucketIdempotentOnConflict(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch r.URL.Path {
		case "/v2/CreateBucket":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"BucketAlreadyExists"}`))
		case "/v2/GetBucketInfo":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(bucketInfo{ID: "bkt-existing", GlobalAliases: []string{"aura-x"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	id, err := c.CreateBucket(context.Background(), "aura-x")
	if err != nil {
		t.Fatalf("CreateBucket(conflict) err = %v, want nil (idempotent)", err)
	}
	if id != "bkt-existing" {
		t.Errorf("CreateBucket(conflict) id = %q, want bkt-existing", id)
	}
}

func TestCreateKeyReturnsCredentials(t *testing.T) {
	c, got := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(keyInfo{AccessKeyID: "GKtest", SecretAccessKey: "s3cr3t"})
	})
	ak, sk, err := c.CreateKey(context.Background(), "key-x")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if ak == "" || sk == "" {
		t.Errorf("CreateKey returned empty creds: ak=%q sk=%q", ak, sk)
	}
	if ak != "GKtest" || sk != "s3cr3t" {
		t.Errorf("CreateKey = (%q,%q), want (GKtest,s3cr3t)", ak, sk)
	}
	if got.Path != "/v2/CreateKey" || got.Body["name"] != "key-x" {
		t.Errorf("request path=%q name=%v, want /v2/CreateKey name=key-x", got.Path, got.Body["name"])
	}
}

// TestAllowBucketKeyGrantsScoped asserts read+write, owner=false on the target bucket.
func TestAllowBucketKeyGrantsScoped(t *testing.T) {
	c, got := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusOK)
	})
	if err := c.AllowBucketKey(context.Background(), "bkt-1", "GKtest", ReadWrite); err != nil {
		t.Fatalf("AllowBucketKey: %v", err)
	}
	if got.Path != "/v2/AllowBucketKey" {
		t.Errorf("path = %q, want /v2/AllowBucketKey", got.Path)
	}
	if got.Body["bucketId"] != "bkt-1" || got.Body["accessKeyId"] != "GKtest" {
		t.Errorf("body bucketId/accessKeyId = %v/%v", got.Body["bucketId"], got.Body["accessKeyId"])
	}
	perms, ok := got.Body["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing/typed wrong: %v", got.Body["permissions"])
	}
	if perms["read"] != true || perms["write"] != true || perms["owner"] != false {
		t.Errorf("permissions = %v, want read+write, owner=false", perms)
	}
}

// TestDeleteInversesIdempotentOn404: deny/delete on a missing bucket/key is success.
func TestDeleteInversesIdempotentOn404(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusNotFound)
	})
	ctx := context.Background()
	if err := c.DeleteBucket(ctx, "bkt-gone"); err != nil {
		t.Errorf("DeleteBucket(404) = %v, want nil (idempotent)", err)
	}
	if err := c.DeleteKey(ctx, "GKgone"); err != nil {
		t.Errorf("DeleteKey(404) = %v, want nil (idempotent)", err)
	}
	if err := c.DenyBucketKey(ctx, "bkt-gone", "GKgone", ReadWrite); err != nil {
		t.Errorf("DenyBucketKey(404) = %v, want nil (idempotent)", err)
	}
}

// TestDeleteSuccessAndBearer: a 2xx delete succeeds and carries the bearer token.
func TestDeleteSuccessAndBearer(t *testing.T) {
	c, got := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusOK)
	})
	if err := c.DeleteKey(context.Background(), "GKtest"); err != nil {
		t.Fatalf("DeleteKey(200) = %v, want nil", err)
	}
	if got.Auth != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want bearer token", got.Auth)
	}
	if !strings.Contains(got.Query, "GKtest") {
		t.Errorf("DeleteKey query = %q, want the key id", got.Query)
	}
}

// TestUnexpectedStatusIsError: a non-2xx that is not the idempotent case surfaces.
func TestUnexpectedStatusIsError(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if err := c.AllowBucketKey(context.Background(), "bkt-1", "GKtest", ReadWrite); err == nil {
		t.Error("AllowBucketKey(500) = nil, want error")
	}
}
