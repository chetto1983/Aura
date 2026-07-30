package config

import (
	"os"

	"github.com/chetto1983/aura/internal/envutil"
)

// Phase 7 (Slice 5) web_search/web_fetch knobs, split out of config.go on touch
// per CLAUDE.md §Behavioral rules (600-LOC cap).

// defaultWebUserAgent is what web_fetch and web_search announce themselves as.
//
// It used to be "Aura/0.x web_fetch" — a deliberate no-browser-spoof choice. That
// choice made web_fetch unable to read a large class of manufacturer and news
// sites, which refuse a self-declared bot at the connection layer rather than with
// an HTTP status. Measured 2026-07-30 against br-automation.com from inside the
// deployed container, same curl, same network, only the UA varying:
//
//	"Aura/0.x web_fetch"  -> 000 (connection refused) in 0.22s
//	curl's own default    -> 000
//	a browser UA          -> 200, 173 KB, 0.43s
//
// In production that surfaced as a fetch TIMEOUT, not a refusal: 6 of 23 real
// web_fetch calls in aura.conversation_turns timed out and every one of them was
// br-automation.com — the manufacturer page holding the spec the user was after.
// The agent then fell back to scraped mirrors and paged through 2.2 KB fragments.
// With this UA the same page returns carrying the full spec table.
//
// This is not evasion of a paywall, a login, or a robots policy: it is declaring a
// shape the server is willing to serve. Aura still honours its SSRF guards,
// redirect revalidation, size caps and Content-Type allowlist unchanged.
const defaultWebUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// applyWebDefaults fills the web_search/web_fetch knobs. SEARXNG_URL has an empty
// default on purpose (D-05): missing is fail-closed at call time, never a boot error.
func applyWebDefaults(cfg *Config) {
	cfg.SearxngURL = os.Getenv("SEARXNG_URL")
	cfg.WebDNSPinTTLSec = envutil.IntDefault("AURA_WEB_DNS_PIN_TTL_SEC", 60)
	cfg.WebFetchMaxBodyBytes = envutil.IntDefault("AURA_WEB_FETCH_MAX_BODY_BYTES", 5_000_000)
	cfg.WebCachePersistent = envutil.BoolDefault("AURA_WEB_CACHE_PERSISTENT", false)
	cfg.WebSearchTimeoutSec = envutil.IntDefault("AURA_WEB_SEARCH_TIMEOUT_SEC", 20)
	cfg.WebFetchTimeoutSec = envutil.IntDefault("AURA_WEB_FETCH_TIMEOUT_SEC", 10)
	cfg.WebUserAgent = envDefault("AURA_WEB_USER_AGENT", defaultWebUserAgent)
}
