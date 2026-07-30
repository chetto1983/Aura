package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

var supportedProtocolVersions = map[string]struct{}{
	"2024-11-05": {},
	"2025-06-18": {},
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools json.RawMessage `json:"tools"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

func validateInitializeResult(raw json.RawMessage) (string, error) {
	var result initializeResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("decode initialize result: %w", err)
	}
	version := strings.TrimSpace(result.ProtocolVersion)
	if _, ok := supportedProtocolVersions[version]; !ok {
		return "", fmt.Errorf("unsupported protocol version %q", version)
	}
	if len(bytes.TrimSpace(result.Capabilities.Tools)) == 0 ||
		bytes.Equal(bytes.TrimSpace(result.Capabilities.Tools), []byte("null")) {
		return "", fmt.Errorf("initialize result missing tools capability")
	}
	if strings.TrimSpace(result.ServerInfo.Name) == "" {
		return "", fmt.Errorf("initialize result missing serverInfo.name")
	}
	if strings.TrimSpace(result.ServerInfo.Version) == "" {
		return "", fmt.Errorf("initialize result missing serverInfo.version")
	}
	return version, nil
}
