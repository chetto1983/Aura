package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/mcp"
)

const databaseMCPServerName = "database"

func handleMCPDatabaseSetupStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := currentDatabaseSetupStatus(deps)
		if err != nil {
			status.Error = err.Error()
		}
		writeJSON(w, deps.Logger, http.StatusOK, status)
	}
}

func handleMCPDatabaseSetupSave(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DatabaseSetupRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		if err := dec.Decode(&req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		path := currentMCPConfigPath(deps)
		if path == "" {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "MCP config path unavailable")
			return
		}
		file, err := readMCPConfigFile(path)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "read MCP config: "+err.Error())
			return
		}
		var existingArgs []string
		if existing, ok := file.MCPServers[databaseMCPServerName]; ok {
			existingArgs = existing.Args
		}
		cfg, err := buildDatabaseMCPConfig(req, defaultDatabaseMCPCommand(deps), existingArgs)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if err := upsertMCPServerConfig(path, databaseMCPServerName, cfg); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "save MCP config: "+err.Error())
			return
		}
		status, err := currentDatabaseSetupStatus(deps)
		if err != nil {
			status.Error = err.Error()
		}
		writeJSON(w, deps.Logger, http.StatusOK, DatabaseSetupResponse{OK: true, Status: status})
	}
}

func currentDatabaseSetupStatus(deps Deps) (DatabaseSetupStatus, error) {
	path := currentMCPConfigPath(deps)
	status := DatabaseSetupStatus{
		CanRestart: deps.Restart != nil,
		Command:    defaultDatabaseMCPCommand(deps),
	}
	status.BinaryPresent = fileExists(status.Command)
	if path == "" {
		return status, errors.New("MCP config path unavailable")
	}
	file, err := readMCPConfigFile(path)
	if err != nil {
		return status, err
	}
	cfg, ok := file.MCPServers[databaseMCPServerName]
	if !ok {
		return status, nil
	}
	status.Configured = true
	status.Command = strings.TrimSpace(cfg.Command)
	applyConfiguredDatabaseArgs(&status, cfg.Args)
	status.BinaryPresent = fileExists(status.Command)
	if provider, ok := findMCPProvider("database"); ok {
		if c := findMCPClientForProvider(provider, deps.MCP); c != nil {
			status.Connected = true
			status.BinaryPresent = true
		}
	}
	status.NeedsRestart = status.Configured && !status.Connected
	status.RestartRequired = status.NeedsRestart
	return status, nil
}

func defaultDatabaseMCPCommand(deps Deps) string {
	if deps.RuntimeConfig != nil {
		root := strings.TrimSpace(deps.RuntimeConfig.RuntimeWorkspacePath)
		if root != "" {
			if filepath.ToSlash(root) == "/workspace" {
				return "/usr/local/bin/ea-database-server"
			}
			name := "ea-database-server"
			if runtime.GOOS == "windows" {
				name += ".cmd"
			}
			return filepath.Join(root, "bin", name)
		}
	}
	return "/usr/local/bin/ea-database-server"
}

func buildDatabaseMCPConfig(req DatabaseSetupRequest, command string, existingArgs []string) (mcp.ServerConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "sqlite"
	}
	if !validDatabaseProvider(provider) {
		return mcp.ServerConfig{}, errors.New("provider must be sqlite, postgresql, mysql, or sqlserver")
	}
	if provider == "sqlite" {
		path := strings.TrimSpace(req.SQLitePath)
		if !validSafeField(path) {
			return mcp.ServerConfig{}, errors.New("SQLite path is required")
		}
		return mcp.ServerConfig{Command: command, Args: []string{path}}, nil
	}
	host := strings.TrimSpace(req.Host)
	if !validSafeField(host) {
		return mcp.ServerConfig{}, errors.New("database host is required")
	}
	database := strings.TrimSpace(req.Database)
	if !validSafeField(database) {
		return mcp.ServerConfig{}, errors.New("database name is required")
	}
	args := []string{"--" + provider}
	if provider == "sqlserver" {
		args = append(args, "--server", host)
	} else {
		args = append(args, "--host", host)
	}
	args = append(args, "--database", database)
	port := firstPositive(req.Port, defaultDatabasePort(provider))
	if port > 0 {
		args = append(args, "--port", strconv.Itoa(port))
	}
	user := strings.TrimSpace(req.User)
	if user != "" {
		if !validSafeField(user) {
			return mcp.ServerConfig{}, errors.New("database user is invalid")
		}
		args = append(args, "--user", user)
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = extractDatabaseArg(existingArgs, "--password")
	}
	if password != "" {
		if !validSafeField(password) {
			return mcp.ServerConfig{}, errors.New("database password is invalid")
		}
		args = append(args, "--password", password)
	}
	if provider == "postgresql" || provider == "mysql" {
		args = append(args, "--ssl", strconv.FormatBool(req.SSL))
	}
	return mcp.ServerConfig{Command: command, Args: args}, nil
}

func applyConfiguredDatabaseArgs(status *DatabaseSetupStatus, args []string) {
	if len(args) == 0 {
		status.Provider = "sqlite"
		return
	}
	switch {
	case args[0] == "--postgresql" || args[0] == "--postgres":
		status.Provider = "postgresql"
	case args[0] == "--mysql":
		status.Provider = "mysql"
	case args[0] == "--sqlserver":
		status.Provider = "sqlserver"
	default:
		status.Provider = "sqlite"
		status.SQLitePath = args[0]
		return
	}
	if status.Provider == "sqlserver" {
		status.Host = extractDatabaseArg(args, "--server")
	} else {
		status.Host = extractDatabaseArg(args, "--host")
	}
	status.Database = extractDatabaseArg(args, "--database")
	status.User = extractDatabaseArg(args, "--user")
	status.PasswordConfigured = extractDatabaseArg(args, "--password") != ""
	status.Port = atoiDefault(extractDatabaseArg(args, "--port"), defaultDatabasePort(status.Provider))
	status.SSL = envBool(extractDatabaseArg(args, "--ssl"))
}

func validDatabaseProvider(provider string) bool {
	switch provider {
	case "sqlite", "postgresql", "mysql", "sqlserver":
		return true
	default:
		return false
	}
}

func defaultDatabasePort(provider string) int {
	switch provider {
	case "postgresql":
		return 5432
	case "mysql":
		return 3306
	case "sqlserver":
		return 1433
	default:
		return 0
	}
}

func extractDatabaseArg(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}
