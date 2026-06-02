//go:build !web_integration

package web

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the web package unit tests under goleak. The Phase 7 engine owns
// a pinned-IP http.Transport (DisableKeepAlives) plus a DNS-pin TTL cache and an
// in-memory response cache; a leaked persistConn, dial goroutine, or cache janitor
// would trip here. Mirrors internal/sandbox/docker_test.go and
// internal/agent/tools/main_test.go.
//
// The live-SearXNG tier lives behind the //go:build web_integration tag (it needs
// a reachable SEARXNG_URL and the public internet); that build carries its own
// goleak TestMain so the integration path is leak-checked too. This untagged file
// covers the SSRF classifier, DNS pin, html extraction, and error-taxonomy
// contracts with httptest + injectable resolvers, no real network.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
