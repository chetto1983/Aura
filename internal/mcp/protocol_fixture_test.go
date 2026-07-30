package mcp

func initializeFixture(version, name string) map[string]any {
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": true},
		},
		"serverInfo": map[string]any{
			"name":    name,
			"version": "1.0.0",
		},
	}
}
