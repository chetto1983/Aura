package main

import (
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

//go:embed integrations_console.html
var integrationsConsoleHTML []byte

// newConsoleHandler builds the validation-console mux: the REAL integrations proxy
// mounted at /api/integrations/ plus the console page served same-origin at "/". Same
// origin is the whole point — a file:// page could not call the token-injecting proxy
// without CORS, so the console rides the proxy's own origin.
func newConsoleHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(integrationsRoutePrefix, newIntegrationsProxy())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(integrationsConsoleHTML)
	})
	return mux
}

// consoleBindGuard keeps the credential-injecting integrations proxy local by
// default. Deliberate network exposure requires both an unsafe flag and auth.
func consoleBindGuard(addr string, unsafeNonLoopback bool, token string) (requireToken bool, err error) {
	host, _, splitErr := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	isLoopback := splitErr == nil &&
		(strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()))
	if isLoopback {
		return false, nil
	}
	if !unsafeNonLoopback {
		return false, fmt.Errorf("non-loopback console bind requires --unsafe-non-loopback")
	}
	if token == "" {
		return false, fmt.Errorf("non-loopback console bind requires AURA_INTEGRATIONS_CONSOLE_TOKEN")
	}
	return true, nil
}

func consoleTokenAuth(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Aura-Console-Token")
		if provided == "" {
			scheme, credentials, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			if ok && strings.EqualFold(scheme, "Bearer") {
				provided = credentials
			}
		}
		providedDigest := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(providedDigest[:], expected[:]) != 1 {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mcpConsole serves the MCP integrations validation console: a local, DB-free HTTP
// server an operator opens to validate both forked sidecars (calendar PIM + whatsapp
// bridge) end to end through the production proxy path. It reads the same env the
// daemon proxy does (AURA_PIM_MCP_PORT/ADMIN_TOKEN, AURA_WHATSAPP_BRIDGE_PORT).
func mcpConsole(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("mcp console", flag.ContinueOnError)
	flags.SetOutput(out)
	addrFlag := flags.String("addr", "127.0.0.1:9099", "HTTP listen address")
	unsafeNonLoopback := flags.Bool("unsafe-non-loopback", false, "allow an authenticated non-loopback bind")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}

	addr := *addrFlag
	token := os.Getenv("AURA_INTEGRATIONS_CONSOLE_TOKEN")
	requireToken, err := consoleBindGuard(addr, *unsafeNonLoopback, token)
	if err != nil {
		return err
	}
	handler := newConsoleHandler()
	if requireToken {
		slog.Warn("MCP validation console is exposed beyond loopback; inbound token authentication is enabled", "addr", addr)
		handler = consoleTokenAuth(token, handler)
	}
	_, _ = fmt.Fprintf(out, "MCP validation console → http://%s/\n", addr)
	_, _ = fmt.Fprintf(out, "  calendar proxy → %s/admin\n", mcpmanager.PIMSidecarBaseURL())
	_, _ = fmt.Fprintf(out, "  whatsapp proxy → %s/api\n", mcpmanager.WhatsAppBridgeBaseURL())
	_, _ = fmt.Fprintln(out, "  (bring both sidecars up first; Ctrl+C to stop)")
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
