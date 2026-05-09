package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/aura/aura/internal/mcp"
)

var errMCPConfigPathUnavailable = errors.New("MCP config path unavailable")

type managedMCPServer struct {
	Name           string
	RuntimeCommand string
	WorkspaceBin   string
	WindowsExt     string
}

func (s managedMCPServer) DefaultCommand(deps Deps) string {
	if deps.RuntimeConfig != nil {
		root := strings.TrimSpace(deps.RuntimeConfig.RuntimeWorkspacePath)
		if root != "" {
			if filepath.ToSlash(root) == "/workspace" {
				return s.RuntimeCommand
			}
			name := s.WorkspaceBin
			if runtime.GOOS == "windows" {
				name += s.WindowsExt
			}
			return filepath.Join(root, "bin", name)
		}
	}
	return s.RuntimeCommand
}

func (s managedMCPServer) ExistingConfig(deps Deps) (mcp.ServerConfig, bool, error) {
	path := currentMCPConfigPath(deps)
	if path == "" {
		return mcp.ServerConfig{}, false, errMCPConfigPathUnavailable
	}
	file, err := readMCPConfigFile(path)
	if err != nil {
		return mcp.ServerConfig{}, false, err
	}
	cfg, ok := file.MCPServers[s.Name]
	return cfg, ok, nil
}

func (s managedMCPServer) UpsertConfig(deps Deps, cfg mcp.ServerConfig) error {
	path := currentMCPConfigPath(deps)
	if path == "" {
		return errMCPConfigPathUnavailable
	}
	return upsertMCPServerConfig(path, s.Name, cfg)
}

func currentMCPConfigPath(deps Deps) string {
	if deps.RuntimeConfig != nil && strings.TrimSpace(deps.RuntimeConfig.MCPServersPath) != "" {
		return deps.RuntimeConfig.MCPServersPath
	}
	return ""
}

func readMCPConfigFile(path string) (mcp.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.File{MCPServers: map[string]mcp.ServerConfig{}}, nil
		}
		return mcp.File{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return mcp.File{MCPServers: map[string]mcp.ServerConfig{}}, nil
	}
	var file mcp.File
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return mcp.File{}, err
	}
	if file.MCPServers == nil {
		file.MCPServers = map[string]mcp.ServerConfig{}
	}
	return file, nil
}

func upsertMCPServerConfig(path, name string, cfg mcp.ServerConfig) error {
	file, err := readMCPConfigFile(path)
	if err != nil {
		return err
	}
	file.MCPServers[name] = cfg
	return writeMCPConfigFile(path, file)
}

func writeMCPConfigFile(path string, file mcp.File) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(file.MCPServers))
	for name := range file.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := struct {
		MCPServers map[string]mcp.ServerConfig `json:"mcpServers"`
	}{MCPServers: make(map[string]mcp.ServerConfig, len(names))}
	for _, name := range names {
		ordered.MCPServers[name] = file.MCPServers[name]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".mcp-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
