package wiki

import (
    "testing"
    "fmt"
)

func TestMarshalMDRoundtrip(t *testing.T) {
    page := &Page{
        Title:         "Test Page",
        Body:          "This is test content.",
        SchemaVersion: CurrentSchemaVersion,
        PromptVersion: "v1",
        CreatedAt:     "2026-04-28T10:00:00Z",
        UpdatedAt:     "2026-04-28T10:00:00Z",
    }
    data, err := MarshalMD(page)
    if err != nil {
        t.Fatalf("MarshalMD failed: %v", err)
    }
    fmt.Printf("=== Marshaled ===\n%s=== End ===\n", string(data))
    
    parsed, err := ParseMD(data)
    if err != nil {
        t.Fatalf("ParseMD failed: %v", err)
    }
    fmt.Printf("Parsed Body: %q\n", parsed.Body)
    if parsed.Body != page.Body {
        t.Errorf("Body = %q, want %q", parsed.Body, page.Body)
    }
}
