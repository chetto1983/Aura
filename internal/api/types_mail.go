package api

// MailSetupStatus is the dashboard-facing state for the guided mail connector
// wizard. Secrets are deliberately omitted; ConfiguredEmail is informational.
type MailSetupStatus struct {
	Configured          bool   `json:"configured"`
	Connected           bool   `json:"connected"`
	NeedsRestart        bool   `json:"needs_restart"`
	RestartRequired     bool   `json:"restart_required"`
	CanRestart          bool   `json:"can_restart"`
	BinaryPresent       bool   `json:"binary_present"`
	Command             string `json:"command,omitempty"`
	Provider            string `json:"provider,omitempty"`
	AccountID           string `json:"account_id,omitempty"`
	Email               string `json:"email,omitempty"`
	ConfiguredEmail     string `json:"configured_email,omitempty"`
	IMAPHost            string `json:"imap_host,omitempty"`
	IMAPPort            int    `json:"imap_port,omitempty"`
	IMAPSecure          bool   `json:"imap_secure,omitempty"`
	SMTPHost            string `json:"smtp_host,omitempty"`
	SMTPPort            int    `json:"smtp_port,omitempty"`
	SMTPSecure          bool   `json:"smtp_secure,omitempty"`
	EnableSMTP          bool   `json:"enable_smtp,omitempty"`
	EnableIMAPMutations bool   `json:"enable_imap_mutations,omitempty"`
	SecretConfigured    bool   `json:"secret_configured,omitempty"`
	Error               string `json:"error,omitempty"`
}

// MailSetupRequest is the guided configuration payload for mail-mcp.
type MailSetupRequest struct {
	Provider            string `json:"provider"` // gmail | outlook | imap
	AccountID           string `json:"account_id,omitempty"`
	Email               string `json:"email"`
	AppPassword         string `json:"app_password"`
	IMAPHost            string `json:"imap_host,omitempty"`
	IMAPPort            int    `json:"imap_port,omitempty"`
	IMAPSecure          *bool  `json:"imap_secure,omitempty"`
	EnableSMTP          bool   `json:"enable_smtp,omitempty"`
	SMTPHost            string `json:"smtp_host,omitempty"`
	SMTPPort            int    `json:"smtp_port,omitempty"`
	SMTPSecure          *bool  `json:"smtp_secure,omitempty"`
	EnableIMAPMutations bool   `json:"enable_imap_mutations,omitempty"`
}

// MailSetupResponse is the body of POST /mcp/mail/setup.
type MailSetupResponse struct {
	OK     bool            `json:"ok"`
	Status MailSetupStatus `json:"status"`
}

// MailMessage is Aura's provider-agnostic mail record.
type MailMessage struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"thread_id,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	Date     string   `json:"date,omitempty"`
	Snippet  string   `json:"snippet,omitempty"`
	Body     string   `json:"body,omitempty"`
}

// MailSearchResponse is the body of GET /mcp/mail/search.
type MailSearchResponse struct {
	ProviderID   string        `json:"provider_id"`
	ProviderTool string        `json:"provider_tool"`
	Messages     []MailMessage `json:"messages"`
	Raw          string        `json:"raw,omitempty"`
}

// MailReadResponse is the body of GET /mcp/mail/{id}.
type MailReadResponse struct {
	ProviderID   string      `json:"provider_id"`
	ProviderTool string      `json:"provider_tool"`
	Message      MailMessage `json:"message"`
	Raw          string      `json:"raw,omitempty"`
}
