package secret

import "testing"

func TestIsSecretEnvKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		// canonical secret set — every marker the three pre-unification
		// detectors covered, plus the bare *_KEY case the shell variant leaked.
		{name: "private key", key: "PRIVATE_KEY", want: true},
		{name: "api key", key: "API_KEY", want: true},
		{name: "openrouter api key", key: "OPENROUTER_API_KEY", want: true},
		{name: "bare key suffix", key: "SSH_KEY", want: true},
		{name: "token suffix", key: "API_TOKEN", want: true},
		{name: "telegram bot token", key: "TELEGRAM_BOT_TOKEN", want: true},
		{name: "secret suffix", key: "CLIENT_SECRET", want: true},
		{name: "password", key: "DB_PASSWORD", want: true},
		{name: "passwd", key: "PASSWD", want: true},
		{name: "passphrase", key: "GPG_PASSPHRASE", want: true},
		{name: "apikey no separator", key: "APIKEY", want: true},
		{name: "authorization", key: "AUTHORIZATION", want: true},
		{name: "bearer", key: "BEARER_TOKEN", want: true},
		{name: "credential", key: "AWS_CREDENTIAL", want: true},
		{name: "credentials plural", key: "GOOGLE_CREDENTIALS", want: true},
		{name: "cert", key: "TLS_CERT", want: true},
		{name: "lowercase input", key: "private_key", want: true},
		{name: "mixed case input", key: "Api_Key", want: true},

		// clear non-secrets — must NOT be over-redacted (normal config).
		{name: "PATH", key: "PATH", want: false},
		{name: "HOME", key: "HOME", want: false},
		{name: "LANG", key: "LANG", want: false},
		{name: "loop steps", key: "AURA_LOOP_MAX_STEPS", want: false},
		{name: "shell timeout", key: "AURA_SHELL_MAX_TIMEOUT_MS", want: false},
		{name: "public host", key: "PUBLIC_HOST", want: false},
		{name: "mail user", key: "MAIL_USER", want: false},
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSecretEnvKey(tt.key); got != tt.want {
				t.Fatalf("IsSecretEnvKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
