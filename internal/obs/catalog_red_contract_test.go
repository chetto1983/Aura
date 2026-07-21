package obs

// Test-only RED contract. Task 2 GREEN replaces this deliberately empty
// catalog with the production descriptor and finite-enum implementation.

import "errors"

type InstrumentID string
type AttributeKey string

const (
	ValueOther                        = "other"
	ToolClassInteraction              = "interaction"
	ToolClassWeb                      = "web"
	ToolClassShell                    = "shell"
	ToolClassFilesystem               = "filesystem"
	ToolClassMCP                      = "mcp"
	AttributeToolClass   AttributeKey = "tool_class"
)

type Descriptor struct {
	ID             InstrumentID
	Name           string
	PrometheusName string
	Attributes     []AttributeKey
}

func CatalogManifest() string                         { return "" }
func ValidateCatalog() error                          { return errors.New("catalog not implemented") }
func Catalog() []Descriptor                           { return nil }
func IsBoundedAttribute(AttributeKey) bool            { return false }
func AllAttributeKeys() []AttributeKey                { return []AttributeKey{AttributeToolClass} }
func NormalizeAttribute(AttributeKey, string) string  { return "" }
func ClassifyTool(string) string                      { return "" }
func AllowedAttributeValue(AttributeKey, string) bool { return false }
