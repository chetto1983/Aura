// config_share.go defines the AURA_SHARE_* operator config surface (Phase 37F,
// WEBSHARE-02/03). Scaffolding only — loadShare() is implemented in the next commit.
package config

// ShareConfig is the AURA_SHARE_* operator surface for Phase 37F conversation/
// artifact sharing (WEBSHARE-02/03).
type ShareConfig struct {
	PublicEnabled bool
	MaxExpiryDays int
}

// loadShare is unimplemented scaffolding (RED gate) — GREEN fills in the real
// envutil-backed parsing.
func loadShare() ShareConfig {
	return ShareConfig{}
}
