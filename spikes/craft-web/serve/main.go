// serve hosts the Craft web builds the way aura does, through internal/staticapp under
// /apps/<app>/, next to a host page that speaks their `?host=parent` protocol, so the spike
// measures the real policy and headers rather than a stand-in server's.
//
//	go run ./spikes/craft-web/serve -apps <dir with one folder per app> -out <dir for saves>
//
// The host page posts every save it receives to /save, which writes the bytes under -out so an
// independent tool can check them.
package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/staticapp"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8790", "listen address")
	apps := flag.String("apps", "", "directory holding one folder per app (pdfcraft, gridcraft, ...)")
	host := flag.String("host", "spikes/craft-web/host", "the host page's directory")
	fixtures := flag.String("fixtures", "web/e2e/fixtures", "files the host page opens")
	out := flag.String("out", "", "directory the received saves are written to")
	flag.Parse()
	if *apps == "" || *out == "" {
		log.Fatal("-apps and -out are required")
	}

	entries, err := os.ReadDir(*apps)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	for _, e := range entries {
		if e.IsDir() {
			route := "/apps/" + e.Name() + "/"
			mux.Handle("GET "+route, staticapp.Handler(route, filepath.Join(*apps, e.Name())))
			log.Printf("serving %s", route)
		}
	}
	mux.Handle("GET /fixtures/", http.StripPrefix("/fixtures/", http.FileServer(http.Dir(*fixtures))))
	mux.Handle("GET /", http.FileServer(http.Dir(*host)))
	mux.HandleFunc("POST /save", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Query().Get("name"))
		if name == "." || name == ".." || name == "/" {
			http.Error(w, "no name", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// #nosec G703 -- name is a single path component (filepath.Base, never . or ..), written inside -out
		if err := os.WriteFile(filepath.Join(*out, name), body, 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	log.Printf("listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux)) // #nosec G114 -- a loopback spike server
}
