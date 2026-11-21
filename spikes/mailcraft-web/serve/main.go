// serve hosts MailCraft's web build the way aura hosts the Craft apps, through internal/staticapp
// under /apps/mailcraft/, beside a minimal JMAP server on the same origin, so the spike measures
// whether the official build runs framed in the cockpit's policy and which JMAP calls it makes.
//
//	go run ./spikes/mailcraft-web/serve -apps <dir with mailcraft/> -log <calls.jsonl>
//
// The app's index.html must load /spike/mailcraft-config.js before its module script: that file
// declares window.mailcraftJmap (apps/mailcraft-web/src/jmap.rs upstream), the hook through which a
// page that has signed its user in hands MailCraft the account and, on demand, the bearer token.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/chetto1983/aura/internal/staticapp"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8790", "listen address")
	apps := flag.String("apps", "", "directory holding mailcraft/ (the unpacked web build)")
	host := flag.String("host", "spikes/mailcraft-web/host", "the framing page's directory")
	calls := flag.String("log", "", "JSON Lines file every JMAP exchange is appended to")
	flag.Parse()
	if *apps == "" || *calls == "" {
		log.Fatal("-apps and -log are required")
	}

	origin := "http://" + *addr
	store := newStore(account, owner)
	jmap, err := newJMAP(origin, store, *calls)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /apps/mailcraft/", staticapp.Handler("/apps/mailcraft/", filepath.Join(*apps, "mailcraft")))
	mux.HandleFunc("GET /spike/mailcraft-config.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, configScript, owner, origin+"/jmap/session")
	})
	mux.HandleFunc("GET /spike/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(token))
	})
	mux.Handle("GET /jmap/", jmap)
	mux.Handle("POST /jmap/", jmap)
	mux.Handle("GET /", http.FileServer(http.Dir(*host)))
	log.Printf("listening on %s", origin)
	log.Fatal(http.ListenAndServe(*addr, mux)) // #nosec G114 -- a loopback spike server
}

// The cockpit would answer secret() with a short-lived token of the signed-in identity; the spike
// hands out one fixed token from its own origin.
const configScript = `window.mailcraftJmap = {
  accounts: [{ email: %q, session: %q, bearer: true, name: "Aura" }],
  secret(email, session) { return fetch("/spike/token", { cache: "no-store" }).then((r) => r.text()); },
};
`

const (
	account = "A1"
	owner   = "ada@aura.test"
	token   = "spike-token"
)
