package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func TestMemoryBatchToolSchema(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "batch-schema", Version: "1"}, nil)
	addMemoryBatchTool(server, nil, testClock, "")
	session := inMemoryIdentityServer(t, server)
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var tool *mcp.Tool
	for _, candidate := range listed.Tools {
		if candidate.Name == "memory_batch" {
			tool = candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("memory_batch is not advertised")
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema = %T", tool.InputSchema)
	}
	properties := schemaMap(t, schema, "properties")
	if got := sortedSchemaKeys(properties); !slices.Equal(got, []string{"idempotency_key", "operations"}) {
		t.Fatalf("top-level properties = %v", got)
	}
	idempotency := schemaMap(t, properties, "idempotency_key")
	if schemaInt(t, idempotency, "minLength") != 1 || schemaInt(t, idempotency, "maxLength") != 100 {
		t.Fatalf("idempotency bounds = %#v", idempotency)
	}
	operations := schemaMap(t, properties, "operations")
	if schemaInt(t, operations, "minItems") != 1 || schemaInt(t, operations, "maxItems") != 100 {
		t.Fatalf("operation bounds = %#v", operations)
	}
	items := schemaMap(t, operations, "items")
	opProperties := schemaMap(t, items, "properties")
	typeSchema := schemaMap(t, opProperties, "type")
	gotKinds := schemaStrings(t, typeSchema, "enum")
	wantKinds := []string{"upsert_fact", "supersede_fact", "merge_entities", "forget"}
	if !slices.Equal(gotKinds, wantKinds) {
		t.Fatalf("operation enum = %v, want %v", gotKinds, wantKinds)
	}
	rawSchema, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	encoded := string(rawSchema)
	for _, forbidden := range []string{"identity_id", "writer_role", "actor_run_id", "transaction"} {
		if strings.Contains(encoded, `"`+forbidden+`"`) {
			t.Fatalf("schema exposes host authority %q: %s", forbidden, encoded)
		}
	}
}

func TestMemoryBatchToolRejectsSpoofedAndMalformedRequests(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "batch-reject", Version: "1"}, nil)
	addMemoryBatchTool(server, nil, testClock, "")
	session := inMemoryIdentityServer(t, server)
	for name, arguments := range map[string]map[string]any{
		"identity spoof": {
			"idempotency_key": "batch-a", "identity_id": testIdentity,
			"operations": []any{map[string]any{"type": "forget", "forget": map[string]any{"subject": "Davide"}}},
		},
		"actor spoof": {
			"idempotency_key": "batch-a", "writer_role": "parent",
			"operations": []any{map[string]any{"type": "forget", "forget": map[string]any{"subject": "Davide"}}},
		},
		"unknown operation": {
			"idempotency_key": "batch-a",
			"operations":      []any{map[string]any{"type": "delete_everything", "forget": map[string]any{"subject": "Davide"}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "memory_batch", Arguments: arguments})
			if err != nil {
				t.Fatalf("CallTool transport: %v", err)
			}
			if !result.IsError {
				t.Fatalf("spoofed or malformed request succeeded: %s", resultText(result))
			}
		})
	}
}

func TestMemoryBatchToolCallsEngineOnce(t *testing.T) {
	called := 0
	wantFirstError := errors.New("first batch error")
	apply := func(
		_ context.Context,
		_ *arcadedb.Client,
		actor arcadedb.MemoryBatchActor,
		request arcadedb.MemoryBatchRequest,
		_ time.Time,
	) (arcadedb.MemoryBatchResult, error) {
		called++
		if actor.IdentityID != testIdentity || actor.WriterRole != arcadedb.WriterParent {
			t.Fatalf("actor = %#v", actor)
		}
		if request.IdempotencyKey != "batch-once" || len(request.Operations) != 4 {
			t.Fatalf("request = %#v", request)
		}
		return arcadedb.MemoryBatchResult{}, wantFirstError
	}
	_, _, err := memoryBatchHandler(singleTenant(t, nil), testClock, "", apply)(
		t.Context(), reqWithParentActor(testIdentity, "batch-parent"), validMemoryBatchInput(),
	)
	if err != wantFirstError {
		t.Fatalf("error = %v, want unchanged first error", err)
	}
	if called != 1 {
		t.Fatalf("ApplyMemoryBatch calls = %d, want 1", called)
	}
}

func TestMemoryBatchToolRejectsBeforeEngine(t *testing.T) {
	called := 0
	apply := func(
		context.Context,
		*arcadedb.Client,
		arcadedb.MemoryBatchActor,
		arcadedb.MemoryBatchRequest,
		time.Time,
	) (arcadedb.MemoryBatchResult, error) {
		called++
		return arcadedb.MemoryBatchResult{}, nil
	}
	input := validMemoryBatchInput()
	input.Operations[0].Merge = &MemoryBatchMergeInput{Source: "a", Target: "b"}
	_, _, malformedErr := memoryBatchHandler(singleTenant(t, nil), testClock, "", apply)(
		t.Context(), reqWithParentActor(testIdentity, "batch-parent"), input,
	)
	if malformedErr == nil {
		t.Fatal("malformed tagged union succeeded")
	}
	_, _, identityErr := memoryBatchHandler(singleTenant(t, nil), testClock, "", apply)(
		t.Context(), reqWithActor("", "batch-parent", string(arcadedb.WriterParent)), validMemoryBatchInput(),
	)
	if identityErr == nil {
		t.Fatal("missing identity succeeded")
	}
	// No bridge headers AND no verified token: nobody vouches for this write.
	// An OAuth-authenticated client is a host in its own right and DOES reach
	// the engine -- see TestUpsertFactDerivesActorFromTheOAuthToken.
	_, _, actorErr := memoryBatchHandler(singleTenant(t, nil), testClock, "", apply)(
		t.Context(), &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{}, Extra: &mcp.RequestExtra{},
		}, validMemoryBatchInput(),
	)
	if actorErr == nil {
		t.Fatal("missing host actor succeeded")
	}
	if called != 0 {
		t.Fatalf("ApplyMemoryBatch called %d times for rejected input", called)
	}
}

func validMemoryBatchInput() MemoryBatchInput {
	fact := func(statement string) *MemoryBatchFactInput {
		return &MemoryBatchFactInput{
			Subject: "Davide", Predicate: "likes", Object: statement, Statement: statement,
			Source: MemoryUpsertFactWriteSource{MemoryIDs: []string{"memory-a"}},
		}
	}
	replacement := fact("Davide likes espresso")
	replacement.SupersedesFactKey = "fact-a"
	return MemoryBatchInput{
		IdempotencyKey: "batch-once",
		Operations: []MemoryBatchOperationInput{
			{Type: "upsert_fact", Fact: fact("Davide likes coffee")},
			{Type: "supersede_fact", Fact: replacement},
			{Type: "merge_entities", Merge: &MemoryBatchMergeInput{Source: "David", Target: "Davide"}},
			{Type: "forget", Forget: &MemoryBatchForgetInput{Subject: "obsolete"}},
		},
	}
}

func schemaMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	result, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("schema %q = %T (%v)", key, value[key], value[key])
	}
	return result
}

func schemaInt(t *testing.T, value map[string]any, key string) int {
	t.Helper()
	number, ok := value[key].(float64)
	if !ok {
		t.Fatalf("schema %q = %T (%v)", key, value[key], value[key])
	}
	return int(number)
}

func schemaStrings(t *testing.T, value map[string]any, key string) []string {
	t.Helper()
	raw, ok := value[key].([]any)
	if !ok {
		t.Fatalf("schema %q = %T (%v)", key, value[key], value[key])
	}
	result := make([]string, len(raw))
	for index, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("schema %q item = %T", key, item)
		}
		result[index] = text
	}
	return result
}

func sortedSchemaKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// The batch write must be able to say everything the single write can say about an
// entity's class. It could not: subject_pole and object_pole existed only on
// memory_upsert_fact, so the BULK path -- the one an import agent uses to coin many
// entities at once -- was the only one that could not state the class of what it coined.
func TestMemoryBatchFactCarriesThePoleClassOntoTheWrite(t *testing.T) {
	fact, err := toMemoryBatchFact(MemoryBatchFactInput{
		Subject: "Marta", SubjectKind: "Officer", SubjectPole: "Person",
		Predicate: "drives",
		Object:    "Volvo", ObjectKind: "Vehicle", ObjectPole: "Object",
		Statement: "Marta drives a Volvo.",
		Source:    MemoryUpsertFactWriteSource{MemoryIDs: []string{"m1"}},
	}, arcadedb.Actor{RunID: "run-1", Role: arcadedb.WriterParent}, "identity-1", "")
	if err != nil {
		t.Fatalf("toMemoryBatchFact: %v", err)
	}
	if fact.SubjectPole != "Person" || fact.ObjectPole != "Object" {
		t.Fatalf("pole classes dropped: subject=%q object=%q", fact.SubjectPole, fact.ObjectPole)
	}
	if fact.SubjectKind != "Officer" || fact.ObjectKind != "Vehicle" {
		t.Fatalf("kinds dropped: subject=%q object=%q", fact.SubjectKind, fact.ObjectKind)
	}
}

// Parity is the invariant, not the two fields: whatever a caller can declare about an
// entity on one write surface it must be able to declare on the other, or the surfaces
// drift again the next time one of them grows a field.
func TestMemoryBatchFactInputMatchesTheSingleWriteEntityFields(t *testing.T) {
	entityField := func(name string) bool {
		return strings.HasPrefix(name, "subject") || strings.HasPrefix(name, "object")
	}
	single := jsonFieldNames(t, MemoryUpsertFactInput{}, entityField)
	batch := jsonFieldNames(t, MemoryBatchFactInput{}, entityField)
	if !slices.Equal(single, batch) {
		t.Fatalf("entity fields differ:\n  memory_upsert_fact: %v\n  memory_batch:       %v", single, batch)
	}
}

// jsonFieldNames lists a struct's json names, keeping the ones select accepts.
func jsonFieldNames(t *testing.T, value any, keep func(string) bool) []string {
	t.Helper()
	structType := reflect.TypeOf(value)
	names := make([]string, 0, structType.NumField())
	for field := range structType.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && keep(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
