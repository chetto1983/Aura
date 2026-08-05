package arcadedb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const documentTestIdentity = "20000000-0000-0000-0000-000000000002"

type tenantResolverFunc func(context.Context, string) (*Client, error)

func (resolve tenantResolverFunc) For(ctx context.Context, identityID string) (*Client, error) {
	return resolve(ctx, identityID)
}

func testDocumentConfig() DocumentIndexConfig {
	return DocumentIndexConfig{
		Dimensions: 3, MaxCandidatePassages: 4, MaxActivePassages: 8,
		MaxRetrievalCandidates: 4, MaxDocumentFilters: 3, MaxQueryRunes: 40,
	}
}

func testDocumentIndex(
	t *testing.T,
	respond func(recordedRequest) testResponse,
	ready bool,
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
	if ready {
		index.schemaReady[documentTestIdentity] = struct{}{}
	}
	return index, requests
}

func TestDocumentSchemaIsExplicitIdempotentAndCached(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	}, false)

	if _, err := index.tenantClient(t.Context(), documentTestIdentity); err != nil {
		t.Fatalf("tenantClient: %v", err)
	}
	firstCount := len(*requests)
	if firstCount == 0 {
		t.Fatal("schema issued no statements")
	}
	var schema strings.Builder
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		schema.WriteString(statement)
		schema.WriteString("\n")
		if strings.HasPrefix(statement, "CREATE") && !strings.Contains(statement, "IF NOT EXISTS") {
			t.Fatalf("non-idempotent DDL: %s", statement)
		}
	}
	joined := schema.String()
	for _, required := range []string{
		"CREATE VERTEX TYPE DocumentProjection",
		"CREATE VERTEX TYPE Passage",
		"CREATE EDGE TYPE HAS_PASSAGE",
		"org.apache.lucene.analysis.standard.StandardAnalyzer",
		"LSM_VECTOR", `"dimensions": 3`, `"quantization": "NONE"`,
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("schema missing %q", required)
		}
	}
	if _, err := index.tenantClient(t.Context(), documentTestIdentity); err != nil {
		t.Fatalf("cached tenantClient: %v", err)
	}
	if len(*requests) != firstCount {
		t.Fatalf("cached schema added %d requests", len(*requests)-firstCount)
	}
}

func TestDocumentSchemaFailureCanBeRetried(t *testing.T) {
	fail := true
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		if fail {
			fail = false
			return testResponse{Status: 500, Body: `{"error":"schema failed"}`}
		}
		return testResponse{Body: `{"result":[]}`}
	}, false)
	if _, err := index.tenantClient(t.Context(), documentTestIdentity); err == nil {
		t.Fatal("schema failure accepted")
	}
	if _, err := index.tenantClient(t.Context(), documentTestIdentity); err != nil {
		t.Fatalf("schema retry: %v", err)
	}
	if len(*requests) <= len(documentSchemaStatements(3)) {
		t.Fatalf("retry did not issue a second schema attempt: %d requests", len(*requests))
	}
}

func TestNewDocumentIndexRejectsInvalidContracts(t *testing.T) {
	resolver := tenantResolverFunc(func(context.Context, string) (*Client, error) { return nil, nil })
	tests := []struct {
		name string
		cfg  DocumentIndexConfig
	}{
		{"dimension", DocumentIndexConfig{}},
		{"negative limit", DocumentIndexConfig{Dimensions: 3, MaxActivePassages: -1}},
		{"candidate above active", DocumentIndexConfig{Dimensions: 3, MaxCandidatePassages: 5, MaxActivePassages: 4}},
	}
	for _, test := range tests {
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
