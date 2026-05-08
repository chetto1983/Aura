package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/mcp"
)

const mailMCPServerName = "mail"

func handleMCPMailSetupStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := currentMailSetupStatus(deps)
		if err != nil {
			status.Error = err.Error()
		}
		writeJSON(w, deps.Logger, http.StatusOK, status)
	}
}

func handleMCPMailSetupSave(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req MailSetupRequest
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
		var existingEnv map[string]string
		if existing, ok := file.MCPServers[mailMCPServerName]; ok {
			existingEnv = existing.Env
		}
		cfg, err := buildMailMCPConfig(req, defaultMailMCPCommand(deps), existingEnv)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if err := upsertMCPServerConfig(path, mailMCPServerName, cfg); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "save MCP config: "+err.Error())
			return
		}
		status, err := currentMailSetupStatus(deps)
		if err != nil {
			status.Error = err.Error()
		}
		writeJSON(w, deps.Logger, http.StatusOK, MailSetupResponse{OK: true, Status: status})
	}
}

func currentMailSetupStatus(deps Deps) (MailSetupStatus, error) {
	path := currentMCPConfigPath(deps)
	status := MailSetupStatus{
		CanRestart: deps.Restart != nil,
		Command:    defaultMailMCPCommand(deps),
	}
	if path == "" {
		return status, errors.New("MCP config path unavailable")
	}
	file, err := readMCPConfigFile(path)
	if err != nil {
		return status, err
	}
	cfg, ok := file.MCPServers[mailMCPServerName]
	if !ok {
		return status, nil
	}
	status.Configured = true
	status.Command = strings.TrimSpace(cfg.Command)
	applyConfiguredMailEnv(&status, cfg.Env)
	status.BinaryPresent = fileExists(status.Command)
	if c := findMCPClientForProvider(mcpProviderManifests[0], deps.MCP); c != nil {
		status.Connected = true
		status.BinaryPresent = true
	}
	status.NeedsRestart = status.Configured && !status.Connected
	status.RestartRequired = status.NeedsRestart
	return status, nil
}

func currentMCPConfigPath(deps Deps) string {
	if deps.RuntimeConfig != nil && strings.TrimSpace(deps.RuntimeConfig.MCPServersPath) != "" {
		return deps.RuntimeConfig.MCPServersPath
	}
	return ""
}

func defaultMailMCPCommand(deps Deps) string {
	if deps.RuntimeConfig != nil {
		root := strings.TrimSpace(deps.RuntimeConfig.RuntimeWorkspacePath)
		if root != "" {
			if filepath.ToSlash(root) == "/workspace" {
				return "/usr/local/bin/mail-mcp"
			}
			name := "mail-mcp"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			return filepath.Join(root, "bin", name)
		}
	}
	return "/usr/local/bin/mail-mcp"
}

func buildMailMCPConfig(req MailSetupRequest, command string, existingEnv map[string]string) (mcp.ServerConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "gmail"
	}
	accountID := normalizeMailAccountID(req.AccountID)
	if accountID == "" {
		accountID = "default"
	}
	if !validMailAccountID(accountID) {
		return mcp.ServerConfig{}, errors.New("account_id can contain only letters, numbers, and underscore")
	}
	email := strings.TrimSpace(req.Email)
	if !validSafeField(email) || !strings.Contains(email, "@") {
		return mcp.ServerConfig{}, errors.New("email is required")
	}
	pass := strings.TrimSpace(req.AppPassword)
	segment := strings.ToUpper(accountID)
	if pass == "" && existingEnv != nil {
		pass = existingEnv["MAIL_IMAP_"+segment+"_PASS"]
	}
	if !validSafeField(pass) {
		return mcp.ServerConfig{}, errors.New("app password is required")
	}
	preset, err := mailProviderPreset(provider)
	if err != nil {
		return mcp.ServerConfig{}, err
	}
	imapHost := firstNonEmpty(req.IMAPHost, preset.imapHost)
	if !validSafeField(imapHost) {
		return mcp.ServerConfig{}, errors.New("IMAP host is required")
	}
	imapPort := firstPositive(req.IMAPPort, preset.imapPort, 993)
	imapSecure := boolEnv(boolWithDefault(req.IMAPSecure, preset.imapSecure), "true", "false")
	env := map[string]string{
		"MAIL_IMAP_" + segment + "_HOST":   imapHost,
		"MAIL_IMAP_" + segment + "_PORT":   strconv.Itoa(imapPort),
		"MAIL_IMAP_" + segment + "_SECURE": imapSecure,
		"MAIL_IMAP_" + segment + "_USER":   email,
		"MAIL_IMAP_" + segment + "_PASS":   pass,
	}
	if req.EnableSMTP {
		smtpHost := firstNonEmpty(req.SMTPHost, preset.smtpHost)
		if !validSafeField(smtpHost) {
			return mcp.ServerConfig{}, errors.New("SMTP host is required when SMTP is enabled")
		}
		smtpPort := firstPositive(req.SMTPPort, preset.smtpPort, 587)
		smtpSecure := boolEnv(boolWithDefault(req.SMTPSecure, preset.smtpSecure), "starttls", "plain")
		env["MAIL_SMTP_"+segment+"_HOST"] = smtpHost
		env["MAIL_SMTP_"+segment+"_PORT"] = strconv.Itoa(smtpPort)
		env["MAIL_SMTP_"+segment+"_SECURE"] = smtpSecure
		env["MAIL_SMTP_"+segment+"_USER"] = email
		env["MAIL_SMTP_"+segment+"_PASS"] = pass
	}
	return mcp.ServerConfig{Command: command, Env: env}, nil
}

type mailPreset struct {
	imapHost   string
	imapPort   int
	imapSecure bool
	smtpHost   string
	smtpPort   int
	smtpSecure bool
}

func mailProviderPreset(provider string) (mailPreset, error) {
	switch provider {
	case "gmail":
		return mailPreset{imapHost: "imap.gmail.com", imapPort: 993, imapSecure: true, smtpHost: "smtp.gmail.com", smtpPort: 587, smtpSecure: true}, nil
	case "outlook":
		return mailPreset{imapHost: "outlook.office365.com", imapPort: 993, imapSecure: true, smtpHost: "smtp.office365.com", smtpPort: 587, smtpSecure: true}, nil
	case "imap":
		return mailPreset{imapPort: 993, imapSecure: true, smtpPort: 587, smtpSecure: true}, nil
	default:
		return mailPreset{}, errors.New("provider must be gmail, outlook, or imap")
	}
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

func applyConfiguredMailEnv(status *MailSetupStatus, env map[string]string) {
	for key, value := range env {
		if strings.HasPrefix(key, "MAIL_IMAP_") && strings.HasSuffix(key, "_USER") {
			segment := strings.TrimSuffix(strings.TrimPrefix(key, "MAIL_IMAP_"), "_USER")
			accountID := strings.ToLower(segment)
			status.AccountID = accountID
			status.Email = value
			status.ConfiguredEmail = value
			status.IMAPHost = env["MAIL_IMAP_"+segment+"_HOST"]
			status.IMAPPort = atoiDefault(env["MAIL_IMAP_"+segment+"_PORT"], 993)
			status.IMAPSecure = envBool(env["MAIL_IMAP_"+segment+"_SECURE"])
			status.SecretConfigured = env["MAIL_IMAP_"+segment+"_PASS"] != ""
			status.Provider = inferMailProvider(status.IMAPHost)
			if smtpUser := env["MAIL_SMTP_"+segment+"_USER"]; smtpUser != "" {
				status.EnableSMTP = true
				status.SMTPHost = env["MAIL_SMTP_"+segment+"_HOST"]
				status.SMTPPort = atoiDefault(env["MAIL_SMTP_"+segment+"_PORT"], 587)
				status.SMTPSecure = env["MAIL_SMTP_"+segment+"_SECURE"] != "plain"
			}
			return
		}
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func normalizeMailAccountID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "_")
	return strings.ToLower(value)
}

func validMailAccountID(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validSafeField(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, "\r\n\x00")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func boolEnv(value bool, trueValue, falseValue string) string {
	if value {
		return trueValue
	}
	return falseValue
}

func boolWithDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func envBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "true", "tls", "starttls", "1", "yes":
		return true
	default:
		return false
	}
}

func atoiDefault(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func inferMailProvider(imapHost string) string {
	host := strings.ToLower(strings.TrimSpace(imapHost))
	switch host {
	case "imap.gmail.com":
		return "gmail"
	case "outlook.office365.com":
		return "outlook"
	default:
		return "imap"
	}
}
