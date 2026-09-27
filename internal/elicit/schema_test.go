package elicit

import (
	"slices"
	"strings"
	"testing"
)

// everythingForm is the reference server's trigger-elicitation-request schema,
// from modelcontextprotocol/servers@baf99300a2c7,
// src/everything/tools/trigger-elicitation-request.ts:56-173, as the SDK hands it to a client.
func everythingForm() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":                       map[string]any{"title": "String", "type": "string", "description": "Your full, legal name"},
			"check":                      map[string]any{"title": "Boolean", "type": "boolean", "description": "Agree to the terms and conditions"},
			"firstLine":                  map[string]any{"title": "String with default", "type": "string", "description": "Favorite first line of a story", "default": "It was a dark and stormy night."},
			"email":                      map[string]any{"title": "String with email format", "type": "string", "format": "email", "description": "Your email address (will be verified, and never shared with anyone else)"},
			"homepage":                   map[string]any{"type": "string", "format": "uri", "title": "String with uri format", "description": "Portfolio / personal website"},
			"birthdate":                  map[string]any{"title": "String with date format", "type": "string", "format": "date", "description": "Your date of birth"},
			"integer":                    map[string]any{"title": "Integer", "type": "integer", "description": "Your favorite integer (do not give us your phone number, pin, or other sensitive info)", "minimum": 1, "maximum": 100, "default": 42},
			"number":                     map[string]any{"title": "Number in range 1-1000", "type": "number", "description": "Favorite number (there are no wrong answers)", "minimum": 0, "maximum": 1000, "default": 3.14},
			"untitledSingleSelectEnum":   map[string]any{"type": "string", "title": "Untitled Single Select Enum", "description": "Choose your favorite friend", "enum": []any{"Monica", "Rachel", "Joey", "Chandler", "Ross", "Phoebe"}, "default": "Monica"},
			"untitledMultipleSelectEnum": map[string]any{"type": "array", "title": "Untitled Multiple Select Enum", "description": "Choose your favorite instruments", "minItems": 1, "maxItems": 3, "items": map[string]any{"type": "string", "enum": []any{"Guitar", "Piano", "Violin", "Drums", "Bass"}}, "default": []any{"Guitar"}},
			"titledSingleSelectEnum":     map[string]any{"type": "string", "title": "Titled Single Select Enum", "description": "Choose your favorite hero", "oneOf": []any{map[string]any{"const": "hero-1", "title": "Superman"}, map[string]any{"const": "hero-2", "title": "Green Lantern"}, map[string]any{"const": "hero-3", "title": "Wonder Woman"}}, "default": "hero-1"},
			"titledMultipleSelectEnum":   map[string]any{"type": "array", "title": "Titled Multiple Select Enum", "description": "Choose your favorite types of fish", "minItems": 1, "maxItems": 3, "items": map[string]any{"anyOf": []any{map[string]any{"const": "fish-1", "title": "Tuna"}, map[string]any{"const": "fish-2", "title": "Salmon"}, map[string]any{"const": "fish-3", "title": "Trout"}}}, "default": []any{"fish-1"}},
			"legacyTitledEnum":           map[string]any{"type": "string", "title": "Legacy Titled Single Select Enum", "description": "Choose your favorite type of pet", "enum": []any{"pet-1", "pet-2", "pet-3", "pet-4", "pet-5"}, "enumNames": []any{"Cats", "Dogs", "Birds", "Fish", "Reptiles"}, "default": "pet-1"},
		},
		"required": []any{"name"},
	}
}

func fromMap(t *testing.T, raw map[string]any) (Question, error) {
	t.Helper()
	schema, err := DecodeSchema(raw)
	if err != nil {
		t.Fatalf("DecodeSchema: %v", err)
	}
	return FromSchema("everything", "trigger-elicitation-request", "Tell me about you", schema)
}

func fieldNamed(t *testing.T, q Question, name string) Field {
	t.Helper()
	for _, f := range q.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %q in %+v", name, q.Fields)
	return Field{}
}

func names(q Question) []string {
	out := make([]string, 0, len(q.Fields))
	for _, f := range q.Fields {
		out = append(out, f.Name)
	}
	return out
}

func TestFromSchemaReadsEveryRestrictedShape(t *testing.T) {
	t.Parallel()
	q, err := fromMap(t, everythingForm())
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	if q.Server != "everything" || q.Tool != "trigger-elicitation-request" || q.Message != "Tell me about you" || q.Schema == nil {
		t.Fatalf("question header/schema = %+v", q)
	}
	want := []string{"name", "birthdate", "check", "email", "firstLine", "homepage", "integer", "legacyTitledEnum", "number", "titledMultipleSelectEnum", "titledSingleSelectEnum", "untitledMultipleSelectEnum", "untitledSingleSelectEnum"}
	if got := names(q); !slices.Equal(got, want) {
		t.Fatalf("field order = %v, want %v", got, want)
	}
	if f := fieldNamed(t, q, "name"); f.Kind != KindString || !f.Required || f.Title != "String" || f.Description != "Your full, legal name" {
		t.Fatalf("name = %+v", f)
	}
	if f := fieldNamed(t, q, "firstLine"); f.Default != "It was a dark and stormy night." || f.Required {
		t.Fatalf("firstLine = %+v", f)
	}
	for name, format := range map[string]string{"email": "email", "homepage": "uri", "birthdate": "date"} {
		if f := fieldNamed(t, q, name); f.Kind != KindString || f.Format != format {
			t.Fatalf("%s = %+v", name, f)
		}
	}
	if f := fieldNamed(t, q, "integer"); f.Kind != KindInteger || *f.Min != 1 || *f.Max != 100 || f.Default != 42.0 {
		t.Fatalf("integer = %+v", f)
	}
	if f := fieldNamed(t, q, "number"); f.Kind != KindNumber || *f.Min != 0 || *f.Max != 1000 {
		t.Fatalf("number = %+v", f)
	}
	if f := fieldNamed(t, q, "check"); f.Kind != KindBoolean || f.Default != nil {
		t.Fatalf("check = %+v", f)
	}
	if f := fieldNamed(t, q, "untitledSingleSelectEnum"); f.Kind != KindEnum || f.Multi || len(f.Enum) != 6 || f.EnumTitles != nil || f.Default != "Monica" {
		t.Fatalf("single enum = %+v", f)
	}
	if f := fieldNamed(t, q, "untitledMultipleSelectEnum"); !f.Multi || strings.Join(f.Enum, ",") != "Guitar,Piano,Violin,Drums,Bass" || *f.MinItems != 1 || *f.MaxItems != 3 {
		t.Fatalf("multi enum = %+v", f)
	}
	if f := fieldNamed(t, q, "titledSingleSelectEnum"); strings.Join(f.Enum, ",") != "hero-1,hero-2,hero-3" || f.EnumTitles[1] != "Green Lantern" {
		t.Fatalf("titled enum = %+v", f)
	}
	if f := fieldNamed(t, q, "titledMultipleSelectEnum"); !f.Multi || f.Enum[2] != "fish-3" || f.EnumTitles[2] != "Trout" || *f.MaxItems != 3 {
		t.Fatalf("titled multi = %+v", f)
	}
	if f := fieldNamed(t, q, "legacyTitledEnum"); strings.Join(f.EnumTitles, ",") != "Cats,Dogs,Birds,Fish,Reptiles" {
		t.Fatalf("legacy enum = %+v", f)
	}
}

func TestFromSchemaPutsRequiredFieldsFirstInTheirOwnOrder(t *testing.T) {
	t.Parallel()
	q, err := fromMap(t, map[string]any{"type": "object", "properties": map[string]any{
		"alpha": map[string]any{"type": "string"}, "beta": map[string]any{"type": "string"}, "gamma": map[string]any{"type": "string"}, "delta": map[string]any{"type": "string"},
	}, "required": []any{"gamma", "alpha", "gamma"}})
	if err != nil || !slices.Equal(names(q), []string{"gamma", "alpha", "beta", "delta"}) {
		t.Fatalf("order = %v, err = %v", names(q), err)
	}
}

func TestFromSchemaNilSchemaIsAMessageOnlyForm(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", "Confirm?", nil)
	if err != nil || q.Message != "Confirm?" || len(q.Fields) != 0 || q.Schema != nil {
		t.Fatalf("FromSchema(nil) = %+v, %v", q, err)
	}
}

func TestFromSchemaRefusesWhatAFormCannotShow(t *testing.T) {
	t.Parallel()
	prop := func(p map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": map[string]any{"f": p}}
	}
	many, heavy := map[string]any{}, map[string]any{}
	for i := range MaxFields + 1 {
		many[string(rune('a'+i))] = map[string]any{"type": "string"}
		if i < MaxFields {
			heavy[string(rune('a'+i))] = map[string]any{"type": "string", "description": strings.Repeat("d", MaxDescriptionBytes)}
		}
	}
	options := make([]any, MaxEnumOptions+1)
	for i := range options {
		options[i] = strings.Repeat("o", i+1)
	}
	for name, raw := range map[string]map[string]any{
		"object":              prop(map[string]any{"type": "object"}),
		"format":              prop(map[string]any{"type": "string", "format": "ipv4"}),
		"bad RE2":             prop(map[string]any{"type": "string", "pattern": "^(?=a)"}),
		"array without items": prop(map[string]any{"type": "array"}),
		"non-string enum":     prop(map[string]any{"type": "string", "enum": []any{1, 2}}),
		"duplicate enum":      prop(map[string]any{"type": "string", "enum": []any{"a", "a"}}),
		"no const":            prop(map[string]any{"type": "string", "oneOf": []any{map[string]any{"title": "x"}}}),
		"field cap":           {"type": "object", "properties": many},
		"option cap":          prop(map[string]any{"type": "string", "enum": options}),
		"question cap":        {"type": "object", "properties": heavy},
		"title cap":           prop(map[string]any{"type": "string", "title": strings.Repeat("t", MaxTitleBytes+1)}),
		"description cap":     prop(map[string]any{"type": "string", "description": strings.Repeat("d", MaxDescriptionBytes+1)}),
		"option label cap":    prop(map[string]any{"type": "string", "enum": []any{strings.Repeat("v", MaxTitleBytes+1)}}),
		"field name cap":      {"type": "object", "properties": map[string]any{strings.Repeat("n", MaxTitleBytes+1): map[string]any{"type": "string"}}},
		"missing required":    {"type": "object", "properties": map[string]any{"f": map[string]any{"type": "string"}}, "required": []any{"g"}},
	} {
		t.Run(name, func(t *testing.T) {
			q, err := fromMap(t, raw)
			if err == nil || q.Message != "" || q.Fields != nil || q.Schema != nil || q.Server != "everything" {
				t.Fatalf("FromSchema accepted or leaked %s: %+v, %v", name, q, err)
			}
		})
	}
}

func TestFromSchemaRefusesAnOverCapMessage(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", strings.Repeat("m", MaxMessageBytes+1), nil)
	if err == nil || q.Message != "" {
		t.Fatalf("over-cap message kept: %v %+v", err, q)
	}
}

func TestDecodeSchemaRefusesWhatIsNotASchema(t *testing.T) {
	t.Parallel()
	if _, err := DecodeSchema(func() {}); err == nil {
		t.Fatal("non-JSON decoded")
	}
	if _, err := DecodeSchema(map[string]any{"type": 5}); err == nil {
		t.Fatal("invalid type decoded")
	}
	if s, err := DecodeSchema(nil); s != nil || err != nil {
		t.Fatalf("DecodeSchema(nil) = %v, %v", s, err)
	}
}
