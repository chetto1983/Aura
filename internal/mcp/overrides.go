package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const ToolOverridesFilename = "tools-overrides.json"

// OverrideWarningReporter exposes unmatched override keys to diagnostics.
type OverrideWarningReporter interface {
	OverrideWarnings() []string
}

// ToolOverridesPath returns the operator-editable sidecar path used for MCP
// description tuning.
func ToolOverridesPath(runtimeWorkspacePath, mcpConfigPath string) string {
	if root := strings.TrimSpace(runtimeWorkspacePath); root != "" {
		return filepath.Join(root, ToolOverridesFilename)
	}
	if configPath := strings.TrimSpace(mcpConfigPath); configPath != "" {
		return filepath.Join(filepath.Dir(configPath), ToolOverridesFilename)
	}
	return ToolOverridesFilename
}

// LoadToolDescriptionOverrides reads tools-overrides.json. Missing or empty
// files mean "no overrides" so operators can remove the file to reset state.
func LoadToolDescriptionOverrides(path string) (map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]string{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read MCP tool overrides %q: %w", path, err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]string{}, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse MCP tool overrides %q: %w", path, err)
	}
	if descriptions, ok := raw["descriptions"]; ok {
		var wrapped map[string]string
		if err := json.Unmarshal(descriptions, &wrapped); err != nil {
			return nil, fmt.Errorf("parse MCP tool overrides descriptions %q: %w", path, err)
		}
		return normalizeToolOverrides(wrapped), nil
	}

	var bare map[string]string
	if err := json.Unmarshal(data, &bare); err != nil {
		return nil, fmt.Errorf("parse MCP tool overrides %q: %w", path, err)
	}
	return normalizeToolOverrides(bare), nil
}

// ApplyToolDescriptionOverrides updates all connected MCP clients and returns
// sorted override keys that did not match any advertised tool.
func ApplyToolDescriptionOverrides(clients []ConnectedClient, overrides map[string]string) []string {
	matched := map[string]struct{}{}
	for _, client := range clients {
		if client == nil {
			continue
		}
		applier, ok := client.(interface {
			ApplyToolDescriptionOverrides(map[string]string) []string
		})
		if !ok {
			continue
		}
		for _, key := range applier.ApplyToolDescriptionOverrides(overrides) {
			matched[key] = struct{}{}
		}
	}

	unmatched := make([]string, 0)
	for key := range overrides {
		if _, ok := matched[key]; !ok {
			unmatched = append(unmatched, key)
		}
	}
	sort.Strings(unmatched)

	for _, client := range clients {
		if client == nil {
			continue
		}
		setter, ok := client.(interface {
			SetOverrideWarnings([]string)
		})
		if ok {
			setter.SetOverrideWarnings(unmatched)
		}
	}
	return unmatched
}

func normalizeToolOverrides(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func toolOverrideKeys(serverName, toolName string) []string {
	serverName = strings.TrimSpace(serverName)
	toolName = strings.TrimSpace(toolName)
	candidates := []string{
		fmt.Sprintf("mcp_%s_%s", serverName, toolName),
		fmt.Sprintf("%s_%s", serverName, toolName),
		toolName,
	}
	out := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, key := range candidates {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}
