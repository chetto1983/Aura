// Spike 009 forward proxy — a ~80-LOC HTTP CONNECT proxy with a hostname
// allowlist, the shape Phase-8 D-08 specced ("host-side Go forward proxy +
// hostname-CONNECT allowlist") and the sandbox-agent pivot dropped. Proves the
// MECHANISM: allowed hosts tunnel, everything else is 403'd at CONNECT time.
//
// Run: go run ./.planning/spikes/009-sandbox-egress-allowlist/proxy   (listens :18443)
// Allowlist via AURA_SPIKE_PROXY_ALLOW (comma host suffixes), default pypi set.
package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func allowed(host string, suffixes []string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		h = h[:i] // strip :443
	}
	for _, s := range suffixes {
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

func main() {
	raw := os.Getenv("AURA_SPIKE_PROXY_ALLOW")
	if raw == "" {
		raw = "pypi.org,files.pythonhosted.org,pythonhosted.org"
	}
	suffixes := strings.Split(raw, ",")
	log.Printf("[proxy] CONNECT allowlist: %v", suffixes)

	srv := &http.Server{
		Addr:         "0.0.0.0:18443",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // tunnels are long-lived
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect {
				http.Error(w, "only CONNECT supported", http.StatusMethodNotAllowed)
				return
			}
			if !allowed(r.Host, suffixes) {
				log.Printf("[proxy] DENY %s", r.Host)
				http.Error(w, "egress host not in allowlist: "+r.Host, http.StatusForbidden)
				return
			}
			log.Printf("[proxy] ALLOW %s", r.Host)
			upstream, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusOK)
			hj, ok := w.(http.Hijacker)
			if !ok {
				_ = upstream.Close()
				return
			}
			client, _, err := hj.Hijack()
			if err != nil {
				_ = upstream.Close()
				return
			}
			go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
			go func() { _, _ = io.Copy(client, upstream); _ = client.Close() }()
		}),
	}
	log.Printf("[proxy] listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
