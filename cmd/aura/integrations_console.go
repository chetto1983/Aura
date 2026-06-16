package main

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
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

// mcpConsole serves the MCP integrations validation console: a local, DB-free HTTP
// server an operator opens to validate both forked sidecars (calendar PIM + whatsapp
// bridge) end to end through the production proxy path. It reads the same env the
// daemon proxy does (AURA_PIM_MCP_PORT/ADMIN_TOKEN, AURA_WHATSAPP_BRIDGE_PORT).
func mcpConsole(args []string, out io.Writer) error {
	addr := "127.0.0.1:9099"
	for i := 0; i < len(args); i++ {
		if args[i] == "--addr" && i+1 < len(args) {
			addr = args[i+1]
			i++
		}
	}
	_, _ = fmt.Fprintf(out, "MCP validation console → http://%s/\n", addr)
	_, _ = fmt.Fprintf(out, "  calendar proxy → %s/admin\n", mcpmanager.PIMSidecarBaseURL())
	_, _ = fmt.Fprintf(out, "  whatsapp proxy → %s/api\n", mcpmanager.WhatsAppBridgeBaseURL())
	_, _ = fmt.Fprintln(out, "  (bring both sidecars up first; Ctrl+C to stop)")
	srv := &http.Server{Addr: addr, Handler: newConsoleHandler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
