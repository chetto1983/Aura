package arcadedb

import (
	"context"
	"errors"
	"testing"
)

const documentTestIdentity = "20000000-0000-0000-0000-000000000002"

type tenantResolverFunc func(context.Context, string) (*Client, error)

func (resolve tenantResolverFunc) For(ctx context.Context, identityID string) (*Client, error) {
	return resolve(ctx, identityID)
}

func testDocumentConfig() DocumentIndexConfig {
	return DocumentIndexConfig{
		Dimensions: 3, MaxRetrievalCandidates: 4, MaxDocumentFilters: 3, MaxQueryRunes: 40,
	}
}

func testDocumentIndex(
	t *testing.T,
	respond func(recordedRequest) testResponse,
) (*DocumentIndex, *[]recordedRequest) {
	t.Helper()
	client, requests := routedClient(t, respond)
	index, err := NewDocumentIndex(tenantResolverFunc(func(_ context.Context, identityID string) (*Client, error) {
		if identityID != documentTestIdentity {
			t.Fatalf("identity = %q", identityID)
		}
		return client, nil
	}), testDocumentConfig())
	if err != nil {
		t.Fatalf("NewDocumentIndex: %v", err)
	}
	return index, requests
}

func TestDocumentIndexDoesNotMutateTheCocoIndexOwnedSchema(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	if _, err := index.tenantClient(t.Context(), documentTestIdentity); err != nil {
		t.Fatalf("tenantClient: %v", err)
	}
	if len(*requests) != 0 {
		t.Fatalf("reader mutated schema with %d HTTP requests", len(*requests))
	}
}

func TestNewDocumentIndexRejectsInvalidContracts(t *testing.T) {
	resolver := tenantResolverFunc(func(context.Context, string) (*Client, error) { return nil, nil })
	for _, test := range []struct {
		name string
		cfg  DocumentIndexConfig
	}{
		{"dimension", DocumentIndexConfig{}},
		{"negative limit", DocumentIndexConfig{Dimensions: 3, MaxRetrievalCandidates: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewDocumentIndex(resolver, test.cfg); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	if _, err := NewDocumentIndex(nil, DocumentIndexConfig{Dimensions: 3}); err == nil {
		t.Fatal("nil resolver accepted")
	}
}

func TestDocumentTenantResolutionFailsClosed(t *testing.T) {
	want := errors.New("resolver refused")
	index, err := NewDocumentIndex(tenantResolverFunc(func(context.Context, string) (*Client, error) {
		return nil, want
	}), DocumentIndexConfig{Dimensions: 3})
	if err != nil {
		t.Fatalf("NewDocumentIndex: %v", err)
	}
	if _, err := index.tenantClient(t.Context(), documentTestIdentity); !errors.Is(err, want) {
		t.Fatalf("resolver error = %v", err)
	}
	nilIndex, err := NewDocumentIndex(tenantResolverFunc(func(context.Context, string) (*Client, error) {
		return nil, nil
	}), DocumentIndexConfig{Dimensions: 3})
	if err != nil {
		t.Fatalf("NewDocumentIndex nil client: %v", err)
	}
	if _, err := nilIndex.tenantClient(t.Context(), documentTestIdentity); err == nil {
		t.Fatal("nil tenant client accepted")
	}
	if _, err := index.tenantClient(t.Context(), "  "); err == nil {
		t.Fatal("empty identity accepted")
	}
}
