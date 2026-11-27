package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/packages/param"
	"github.com/chetto1983/aura/internal/llm"
)

// combinators are the schema keywords the API refuses at the top of input_schema, which
// must be a plain object schema.
var combinators = []string{"anyOf", "oneOf", "allOf", "not"}

// convertTools declares each named function once, first declaration wins: the API
// refuses a request that names a tool twice.
func convertTools(defs []llm.ToolDef) ([]anthropic.BetaToolUnionParam, error) {
	var out []anthropic.BetaToolUnionParam
	seen := map[string]bool{}
	for _, def := range defs {
		name := def.Function.Name
		if def.Type != "function" || name == "" {
			return nil, errors.New("anthropic: only named function tools are supported")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		schema, description, err := inputSchema(def.Function.Parameters, def.Function.Description)
		if err != nil {
			return nil, fmt.Errorf("anthropic: tool %s: %w", name, err)
		}
		tool := &anthropic.BetaToolParam{Name: name, InputSchema: schema}
		if description != "" {
			tool.Description = param.NewOpt(description)
		}
		out = append(out, anthropic.BetaToolUnionParam{OfTool: tool})
	}
	return out, nil
}

// inputSchema sends the tool's schema as an object schema. Its bytes are re-encoded with
// sorted keys: the SDK writes a schema's extra fields in map order, which would change
// the cached tools prefix on every request. A schema the API would refuse at the top
// (a combinator, a non-object type) becomes an open object, and the model reads the
// real schema in the description instead.
func inputSchema(raw json.RawMessage, description string) (anthropic.BetaToolInputSchemaParam, string, error) {
	schema := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if !json.Valid(raw) {
			return anthropic.BetaToolInputSchemaParam{}, "", errors.New("invalid schema: not JSON")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&schema); err != nil {
			return anthropic.BetaToolInputSchemaParam{}, "", fmt.Errorf("invalid schema: %w", err)
		}
	}
	if !objectSchema(schema) {
		var compact bytes.Buffer
		_ = json.Compact(&compact, raw) // raw passed json.Valid above, so Compact cannot fail
		description = strings.TrimSpace(description + "\n\nInput JSON schema: " + compact.String())
		schema = nil
	}
	if schema == nil {
		schema = map[string]any{}
	}
	schema["type"] = "object"
	encoded, err := json.Marshal(schema)
	if err != nil {
		return anthropic.BetaToolInputSchemaParam{}, "", fmt.Errorf("invalid schema: %w", err)
	}
	return param.Override[anthropic.BetaToolInputSchemaParam](json.RawMessage(encoded)), description, nil
}

func objectSchema(schema map[string]any) bool {
	for _, keyword := range combinators {
		if _, ok := schema[keyword]; ok {
			return false
		}
	}
	kind, ok := schema["type"]
	return !ok || kind == "object"
}
