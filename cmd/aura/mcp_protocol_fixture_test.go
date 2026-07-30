package main

func mcpInitializeResult(version, name string) map[string]any {
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": name, "version": "1.0.0"},
	}
}
