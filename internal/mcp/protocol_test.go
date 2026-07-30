package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateInitializeResult(t *testing.T) {
	t.Parallel()
	valid := func(version string) json.RawMessage {
		return json.RawMessage(`{
			"protocolVersion":` + strconvQuote(version) + `,
			"capabilities":{"tools":{"listChanged":true}},
			"serverInfo":{"name":"fixture","version":"1.2.3"}
		}`)
	}
	for _, version := range []string{"2024-11-05", "2025-06-18"} {
		t.Run("supported_"+version, func(t *testing.T) {
			t.Parallel()
			got, err := validateInitializeResult(valid(version))
			if err != nil {
				t.Fatalf("validate supported initialize: %v", err)
			}
			if got != version {
				t.Fatalf("negotiated version = %q, want %q", got, version)
			}
		})
	}

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"malformed", `{`, "decode initialize result"},
		{"unsupported_version", `{"protocolVersion":"2099-01-01","capabilities":{"tools":{}},"serverInfo":{"name":"x","version":"1"}}`, "unsupported protocol version"},
		{"missing_tools", `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"x","version":"1"}}`, "tools capability"},
		{"missing_server_name", `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"version":"1"}}`, "serverInfo.name"},
		{"missing_server_version", `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"x"}}`, "serverInfo.version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := validateInitializeResult(json.RawMessage(tc.raw)); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
