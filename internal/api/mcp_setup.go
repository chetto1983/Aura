package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/mcppolicy"
)

const mailMCPServerName = "mail"

var mailMCPServer = managedMCPServer{
	Name:           mailMCPServerName,
	RuntimeCommand: "/usr/local/bin/mail-mcp",
	WorkspaceBin:   "mail-mcp",
	WindowsExt:     ".exe",
}

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
		existing, _, err := mailMCPServer.ExistingConfig(deps)
		if errors.Is(err, errMCPConfigPathUnavailable) {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "MCP config path unavailable")
			return
		}
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "read MCP config: "+err.Error())
			return
		}
		cfg, err := buildMailMCPConfig(req, defaultMailMCPCommand(deps), existing.Env)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if err := mailMCPServer.UpsertConfig(deps, cfg); err != nil {
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
	status := MailSetupStatus{
		CanRestart: deps.Restart != nil,
		Command:    defaultMailMCPCommand(deps),
	}
	status.BinaryPresent = fileExists(status.Command)
	cfg, ok, err := mailMCPServer.ExistingConfig(deps)
	if err != nil {
		return status, err
	}
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

func defaultMailMCPCommand(deps Deps) string {
	return mailMCPServer.DefaultCommand(deps)
}

func buildMailMCPConfig(req MailSetupRequest, command string, existingEnv map[string]string) (mcp.ServerConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "gmail"
	}
	accountID := normalizeMailAccountID(req.AccountID)
	if accountID == "" {
		accountID = existingMailAccountID(existingEnv)
	}
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
	if req.EnableIMAPMutations {
		env["AURA_MAIL_"+segment+"_ENABLE_IMAP_MUTATIONS"] = "true"
		env[mcppolicy.MailIMAPWriteEnabledEnv] = "true"
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
			status.EnableIMAPMutations = envFlagBool(env["AURA_MAIL_"+segment+"_ENABLE_IMAP_MUTATIONS"]) || envFlagBool(env[mcppolicy.MailIMAPWriteEnabledEnv])
			return
		}
	}
}

func normalizeMailAccountID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "_")
	return strings.ToLower(value)
}

func existingMailAccountID(env map[string]string) string {
	for key := range env {
		if strings.HasPrefix(key, "MAIL_IMAP_") && strings.HasSuffix(key, "_USER") {
			segment := strings.TrimSuffix(strings.TrimPrefix(key, "MAIL_IMAP_"), "_USER")
			return normalizeMailAccountID(segment)
		}
	}
	return ""
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

func envFlagBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on", "enabled":
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
