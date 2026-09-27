package elicit

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// Every part supplied by a server is bounded so a form is declined intact
// instead of shown with text cut off. The total bound protects the replay ring.
const (
	MaxFields           = 20
	MaxEnumOptions      = 50
	MaxMessageBytes     = 2 << 10
	MaxTitleBytes       = 256
	MaxDescriptionBytes = 1 << 10
	MaxAnswerBytes      = 4 << 10
	MaxQuestionBytes    = 16 << 10
	// Concurrent SDK requests could otherwise hold unbounded cards and clocks.
	MaxOpenQuestions = 4
)

var formats = []string{"email", "uri", "date", "date-time"}

// DecodeSchema turns the SDK's map-shaped requestedSchema into a typed schema.
func DecodeSchema(raw any) (*jsonschema.Schema, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("requested schema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("requested schema: %w", err)
	}
	return &schema, nil
}

// FromSchema projects a requested schema into bounded fields. The SDK hands
// requestedSchema over as a map, losing property key order. The
// required array retains its order; the rest of the fields follow by name.
func FromSchema(server, tool, message string, schema *jsonschema.Schema) (Question, error) {
	refused := Question{Server: server, Tool: tool}
	if len(message) > MaxMessageBytes {
		return refused, fmt.Errorf("message is %d bytes, over %d", len(message), MaxMessageBytes)
	}
	q := Question{Server: server, Tool: tool, Message: message, Fields: []Field{}}
	if schema != nil {
		fields, err := fieldsOf(schema)
		if err != nil {
			return refused, err
		}
		// Resolve here because the SDK otherwise does so after the handler returns.
		// An invalid RE2 pattern must be refused before an operator sees the form.
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return refused, fmt.Errorf("schema does not resolve: %w", err)
		}
		q.Fields, q.Schema = fields, resolved
	}
	data, err := json.Marshal(q)
	if err != nil {
		return refused, fmt.Errorf("form does not encode: %w", err)
	}
	if len(data) > MaxQuestionBytes {
		return refused, fmt.Errorf("form is %d bytes, over %d", len(data), MaxQuestionBytes)
	}
	return q, nil
}

func fieldsOf(schema *jsonschema.Schema) ([]Field, error) {
	if len(schema.Properties) > MaxFields {
		return nil, fmt.Errorf("form has %d fields, over %d", len(schema.Properties), MaxFields)
	}
	order, err := fieldOrder(schema)
	if err != nil {
		return nil, err
	}
	fields := make([]Field, 0, len(order))
	for _, name := range order {
		if len(name) > MaxTitleBytes {
			return nil, fmt.Errorf("field name is %d bytes, over %d", len(name), MaxTitleBytes)
		}
		field, err := fieldOf(name, schema.Properties[name], slices.Contains(schema.Required, name))
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func fieldOrder(schema *jsonschema.Schema) ([]string, error) {
	order := make([]string, 0, len(schema.Properties))
	for _, name := range schema.Required {
		if _, defined := schema.Properties[name]; !defined {
			return nil, fmt.Errorf("required field %q is not in form", name)
		}
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	return order, nil
}

func fieldOf(name string, p *jsonschema.Schema, required bool) (Field, error) {
	if p == nil {
		return Field{}, errors.New("field has no schema")
	}
	if err := capped("title", p.Title, MaxTitleBytes); err != nil {
		return Field{}, err
	}
	if err := capped("description", p.Description, MaxDescriptionBytes); err != nil {
		return Field{}, err
	}
	f := Field{Name: name, Title: p.Title, Description: p.Description, Required: required}
	if len(p.Default) > 0 {
		if err := json.Unmarshal(p.Default, &f.Default); err != nil {
			return Field{}, fmt.Errorf("default: %w", err)
		}
	}
	switch p.Type {
	case "boolean":
		f.Kind = KindBoolean
	case "number", "integer":
		f.Kind = Kind(p.Type)
		f.Min, f.Max = p.Minimum, p.Maximum
	case "string":
		if len(p.Enum) > 0 || len(p.OneOf) > 0 {
			return enumField(f, p)
		}
		if p.Format != "" && !slices.Contains(formats, p.Format) {
			return Field{}, fmt.Errorf("format %q is unsupported", p.Format)
		}
		f.Kind, f.Format, f.Pattern = KindString, p.Format, p.Pattern
		f.MinLength, f.MaxLength = p.MinLength, p.MaxLength
	case "array":
		if p.Items == nil {
			return Field{}, errors.New("array has no items")
		}
		f.Multi, f.MinItems, f.MaxItems = true, p.MinItems, p.MaxItems
		return enumField(f, p.Items)
	default:
		return Field{}, fmt.Errorf("type %q is unsupported", p.Type)
	}
	return f, nil
}

func enumField(f Field, p *jsonschema.Schema) (Field, error) {
	f.Kind = KindEnum
	switch {
	case len(p.Enum) > 0:
		for _, v := range p.Enum {
			s, ok := v.(string)
			if !ok {
				return Field{}, fmt.Errorf("enum value %v is not a string", v)
			}
			f.Enum = append(f.Enum, s)
		}
		if names, ok := p.Extra["enumNames"].([]any); ok {
			for _, n := range names {
				s, ok := n.(string)
				if !ok {
					return Field{}, fmt.Errorf("enumNames entry %v is not a string", n)
				}
				f.EnumTitles = append(f.EnumTitles, s)
			}
		}
	case len(p.OneOf) > 0 || len(p.AnyOf) > 0:
		for _, entry := range slices.Concat(p.OneOf, p.AnyOf) {
			value, ok := constString(entry)
			if !ok {
				return Field{}, errors.New("option has no string const")
			}
			f.Enum = append(f.Enum, value)
			f.EnumTitles = append(f.EnumTitles, entry.Title)
		}
	default:
		return Field{}, errors.New("field offers no options")
	}
	if len(f.Enum) > MaxEnumOptions {
		return Field{}, fmt.Errorf("field has %d options, over %d", len(f.Enum), MaxEnumOptions)
	}
	// Duplicate values cannot be distinguished once chosen.
	if len(slices.Compact(slices.Sorted(slices.Values(f.Enum)))) != len(f.Enum) {
		return Field{}, errors.New("two options have the same value")
	}
	for _, label := range slices.Concat(f.Enum, f.EnumTitles) {
		if err := capped("option", label, MaxTitleBytes); err != nil {
			return Field{}, err
		}
	}
	return f, nil
}

func constString(entry *jsonschema.Schema) (string, bool) {
	if entry == nil || entry.Const == nil {
		return "", false
	}
	s, ok := (*entry.Const).(string)
	return s, ok
}

func capped(what, s string, limit int) error {
	if len(s) > limit {
		return fmt.Errorf("%s is %d bytes, over %d", what, len(s), limit)
	}
	return nil
}
