package elicit

import (
	"context"
	"errors"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// Kind is the input shape the restricted MCP form can render.
type Kind string

// Kind constants are the restricted property types the MCP form can show.
const (
	KindString  Kind = "string"
	KindNumber  Kind = "number"
	KindInteger Kind = "integer"
	KindBoolean Kind = "boolean"
	KindEnum    Kind = "enum"
)

// Action constants are the three MCP elicitation decisions.
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// A refusal code lets each surface explain why Aura declined in its own language.
const (
	RefusalUnrenderable = "unrenderable"
	RefusalAmbiguousRun = "ambiguous_run"
)

// ErrExpired identifies a wait ended by Aura's question deadline.
var ErrExpired = errors.New("elicitation expired")

// Field is a bounded top-level property ready to show to an operator.
type Field struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Kind        Kind     `json:"kind"`
	Required    bool     `json:"required"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	EnumTitles  []string `json:"enum_titles,omitempty"`
	Multi       bool     `json:"multi,omitempty"`
	Format      string   `json:"format,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	MinLength   *int     `json:"min_length,omitempty"`
	MaxLength   *int     `json:"max_length,omitempty"`
	MinItems    *int     `json:"min_items,omitempty"`
	MaxItems    *int     `json:"max_items,omitempty"`
	// Go's RE2 pattern stays server-side; browser patterns use ECMAScript.
	Pattern string `json:"-"`
}

// Question is the bounded form and its server-side resolved schema.
type Question struct {
	ID       string               `json:"id"`
	Server   string               `json:"server"`
	Tool     string               `json:"tool,omitempty"`
	Message  string               `json:"message"`
	Fields   []Field              `json:"fields"`
	Deadline time.Time            `json:"deadline"`
	Refusal  string               `json:"refusal,omitempty"`
	Schema   *jsonschema.Resolved `json:"-"`
}

// Answer carries the operator's decision and accepted content.
type Answer struct {
	Action  string
	Content map[string]any
}

// Asker is bound to one run. A cancelled wait returns context.Cause(ctx).
type Asker interface {
	Ask(ctx context.Context, q Question) (Answer, error)
}

type askerKey struct{}

// WithAsker installs the asker of the run carried by ctx.
func WithAsker(ctx context.Context, asker Asker) context.Context {
	return context.WithValue(ctx, askerKey{}, asker)
}

// AskerFrom returns the run's asker, or nil when no surface can ask.
func AskerFrom(ctx context.Context) Asker {
	asker, _ := ctx.Value(askerKey{}).(Asker)
	return asker
}
