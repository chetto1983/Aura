package tools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

func TestValidateBeforeCall(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer"},
			"score": map[string]any{"type": "number"},
			"fresh": map[string]any{"type": "boolean"},
			"mode":  map[string]any{"type": "string", "enum": []string{"fast", "deep"}},
			"filter": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{"type": "string"},
				},
				"required": []string{"kind"},
			},
			"ids": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer"},
			},
		},
		"required": []string{"query"},
	}

	tests := []struct {
		name             string
		params           map[string]any
		schema           map[string]any
		wantValid        bool
		wantMissing      []string
		wantInvalidType  []string
		wantInvalidEnum  []string
		wantRecoverable  bool
		wantHintContains string
	}{
		{
			name:             "missing required key",
			params:           map[string]any{},
			schema:           schema,
			wantMissing:      []string{"query"},
			wantHintContains: "provide required",
			wantRecoverable:  true,
		},
		{
			name:             "nil required key counts as missing",
			params:           map[string]any{"query": nil},
			schema:           schema,
			wantMissing:      []string{"query"},
			wantRecoverable:  true,
			wantHintContains: "query",
		},
		{
			name:            "invalid string type",
			params:          map[string]any{"query": 42},
			schema:          schema,
			wantInvalidType: []string{"query"},
			wantRecoverable: true,
		},
		{
			name:            "invalid integer type",
			params:          map[string]any{"query": "x", "limit": 1.5},
			schema:          schema,
			wantInvalidType: []string{"limit"},
			wantRecoverable: true,
		},
		{
			name:      "integer accepts integral numeric values",
			params:    map[string]any{"query": "x", "limit": 2.0},
			schema:    schema,
			wantValid: true,
		},
		{
			name:      "number accepts int",
			params:    map[string]any{"query": "x", "score": 2},
			schema:    schema,
			wantValid: true,
		},
		{
			name:            "invalid boolean type",
			params:          map[string]any{"query": "x", "fresh": "true"},
			schema:          schema,
			wantInvalidType: []string{"fresh"},
			wantRecoverable: true,
		},
		{
			name:            "invalid enum",
			params:          map[string]any{"query": "x", "mode": "wide"},
			schema:          schema,
			wantInvalidEnum: []string{"mode"},
			wantRecoverable: true,
		},
		{
			name:            "combined missing and enum",
			params:          map[string]any{"mode": "wide"},
			schema:          schema,
			wantMissing:     []string{"query"},
			wantInvalidEnum: []string{"mode"},
			wantRecoverable: true,
		},
		{
			name:      "schema with no constraints passes through",
			params:    map[string]any{"anything": "goes"},
			schema:    map[string]any{"type": "object"},
			wantValid: true,
		},
		{
			name:      "nil params graceful",
			params:    nil,
			schema:    map[string]any{"type": "object"},
			wantValid: true,
		},
		{
			name:      "nil schema pass-through",
			params:    map[string]any{"query": 42},
			schema:    nil,
			wantValid: true,
		},
		{
			name:             "nested required path",
			params:           map[string]any{"query": "x", "filter": map[string]any{}},
			schema:           schema,
			wantMissing:      []string{"filter.kind"},
			wantRecoverable:  true,
			wantHintContains: "filter.kind",
		},
		{
			name:            "array item type path",
			params:          map[string]any{"query": "x", "ids": []any{1, "two"}},
			schema:          schema,
			wantInvalidType: []string{"ids[1]"},
			wantRecoverable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateBeforeCall("search_memory", tt.params, tt.schema)
			if got.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; result=%+v", got.Valid, tt.wantValid, got)
			}
			if !sameStrings(got.Missing, tt.wantMissing) {
				t.Fatalf("Missing = %#v, want %#v", got.Missing, tt.wantMissing)
			}
			if !sameStrings(typeFields(got.InvalidType), tt.wantInvalidType) {
				t.Fatalf("InvalidType fields = %#v, want %#v", typeFields(got.InvalidType), tt.wantInvalidType)
			}
			if !sameStrings(enumFields(got.InvalidEnum), tt.wantInvalidEnum) {
				t.Fatalf("InvalidEnum fields = %#v, want %#v", enumFields(got.InvalidEnum), tt.wantInvalidEnum)
			}
			if got.Recoverable != tt.wantRecoverable {
				t.Fatalf("Recoverable = %v, want %v", got.Recoverable, tt.wantRecoverable)
			}
			if tt.wantHintContains != "" && !strings.Contains(got.Hint, tt.wantHintContains) {
				t.Fatalf("Hint = %q, want substring %q", got.Hint, tt.wantHintContains)
			}
		})
	}
}

func TestCoerceParams(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":   map[string]any{"type": "integer"},
			"ratio":   map[string]any{"type": "number"},
			"deliver": map[string]any{"type": "boolean"},
			"mode":    map[string]any{"type": "string", "enum": []string{"1", "2"}},
			"filter": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{"type": "integer"},
				},
			},
			"ids": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer"},
			},
		},
	}
	in := map[string]any{
		"limit":   "5",
		"ratio":   "0.75",
		"deliver": "true",
		"mode":    2,
		"filter":  map[string]any{"count": "3"},
		"ids":     []any{"1", "2"},
	}

	got := CoerceParams(in, schema)
	if got["limit"] != 5 {
		t.Fatalf("limit = %#v, want int(5)", got["limit"])
	}
	if got["ratio"] != 0.75 {
		t.Fatalf("ratio = %#v, want float64(0.75)", got["ratio"])
	}
	if got["deliver"] != true {
		t.Fatalf("deliver = %#v, want true", got["deliver"])
	}
	if got["mode"] != "2" {
		t.Fatalf("mode = %#v, want string enum value", got["mode"])
	}
	filter, _ := got["filter"].(map[string]any)
	if filter["count"] != 3 {
		t.Fatalf("filter.count = %#v, want int(3)", filter["count"])
	}
	if !reflect.DeepEqual(got["ids"], []any{1, 2}) {
		t.Fatalf("ids = %#v, want []any{1, 2}", got["ids"])
	}
	if in["limit"] != "5" {
		t.Fatalf("CoerceParams mutated original input: %#v", in)
	}
}

func TestValidationErrorFormatsStructuredToolResult(t *testing.T) {
	err := &ValidationError{
		Tool: "fake",
		Result: ValidationResult{
			Valid:       false,
			Missing:     []string{"query"},
			InvalidType: []TypeMismatch{},
			InvalidEnum: []EnumMismatch{},
			Hint:        "provide query",
			Recoverable: true,
		},
	}
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatal("ValidationError should unwrap to llm.ErrSchemaValidation")
	}
	var payload struct {
		Tool            string `json:"tool"`
		ValidationError struct {
			Missing     []string `json:"missing"`
			Recoverable bool     `json:"recoverable"`
		} `json:"validation_error"`
	}
	if jsonErr := json.Unmarshal([]byte(FormatToolError(err)), &payload); jsonErr != nil {
		t.Fatalf("FormatToolError validation payload is not JSON: %v", jsonErr)
	}
	if payload.Tool != "fake" || !reflect.DeepEqual(payload.ValidationError.Missing, []string{"query"}) || !payload.ValidationError.Recoverable {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestRegistryExecutePrecallValidator(t *testing.T) {
	t.Setenv("AURA_OP12_PRECALL_VALIDATOR_ENABLED", "true")
	_, identityStore, actor := newRegistryIdentityEnv(t)
	reg := NewRegistry(nil)
	tool := &precallRecordingTool{}
	reg.Register(tool)
	grantToolExecute(t, identityStore, actor, "precall_fake")

	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), identityStore), actor.ID)
	_, err := reg.Execute(ctx, "precall_fake", map[string]any{"mode": "deep"})
	if err == nil {
		t.Fatal("Execute returned nil error, want validation error")
	}
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("err = %v, want ErrSchemaValidation", err)
	}
	if tool.calls != 0 {
		t.Fatalf("tool executed despite validation failure")
	}
	if got := FormatToolError(err); !strings.Contains(got, `"validation_error"`) || !strings.Contains(got, `"query"`) {
		t.Fatalf("formatted validation error = %q", got)
	}

	result, err := reg.Execute(ctx, "precall_fake", map[string]any{"query": "docs", "limit": "5", "mode": "deep"})
	if err != nil {
		t.Fatalf("valid coerced call error: %v", err)
	}
	if result != "ok" || tool.calls != 1 {
		t.Fatalf("result=%q calls=%d, want ok/1", result, tool.calls)
	}
	if tool.lastArgs["limit"] != 5 {
		t.Fatalf("coerced limit = %#v, want int(5)", tool.lastArgs["limit"])
	}
}

func TestRegistryExecutePrecallValidatorDisabledByDefault(t *testing.T) {
	t.Setenv("AURA_OP12_PRECALL_VALIDATOR_ENABLED", "false")
	_, identityStore, actor := newRegistryIdentityEnv(t)
	reg := NewRegistry(nil)
	tool := &precallRecordingTool{}
	reg.Register(tool)
	grantToolExecute(t, identityStore, actor, "precall_fake")

	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), identityStore), actor.ID)
	if _, err := reg.Execute(ctx, "precall_fake", map[string]any{"mode": "deep"}); err != nil {
		t.Fatalf("validator disabled Execute error = %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("tool calls = %d, want 1 when validator disabled", tool.calls)
	}
}

func TestRegistryExecuteSuccessDoesNotAppendRetryHint(t *testing.T) {
	t.Setenv("AURA_OP12B_RETRY_HINT_ENABLED", "true")
	_, identityStore, actor := newRegistryIdentityEnv(t)
	reg := NewRegistry(nil)
	tool := &precallRecordingTool{}
	reg.Register(tool)
	grantToolExecute(t, identityStore, actor, "precall_fake")

	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), identityStore), actor.ID)
	got, err := reg.Execute(ctx, "precall_fake", map[string]any{"query": "docs"})
	if err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	if strings.Contains(got, retryHintSuffix) {
		t.Fatalf("success result has retry hint: %q", got)
	}
}

type precallRecordingTool struct {
	calls    int
	lastArgs map[string]any
}

func (t *precallRecordingTool) Name() string        { return "precall_fake" }
func (t *precallRecordingTool) Description() string { return "Precall fake" }
func (t *precallRecordingTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer"},
			"mode":  map[string]any{"type": "string", "enum": []string{"fast", "deep"}},
		},
		"required": []string{"query"},
	}
}
func (t *precallRecordingTool) Execute(_ context.Context, args map[string]any) (string, error) {
	t.calls++
	t.lastArgs = cloneAnyMap(args)
	return "ok", nil
}

func grantToolExecute(t *testing.T, store *identity.Store, actor identity.Actor, toolName string) {
	t.Helper()
	_, err := store.CreateGrant(context.Background(), identity.GrantParams{
		ID:          "grant:test:" + toolName,
		SubjectType: identity.SubjectTypePrincipal,
		SubjectID:   actor.PrincipalID,
		Capability:  identity.CapabilityToolExecute,
		Resource:    identity.ResourceRef{Type: "tool", ID: toolName},
	})
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
}

func typeFields(items []TypeMismatch) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Field)
	}
	return out
}

func enumFields(items []EnumMismatch) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Field)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
