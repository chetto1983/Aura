// aura-filecard renders one file's card on stdout.
//
// It exists so the ingest sidecar can build cards WITHOUT a second implementation of
// filecard. The card is what makes a spreadsheet answerable: measured on a 500-row,
// 3-sheet workbook it finds the real header under a merged banner, types every column,
// and reports value distributions -- "Citta: most common Torino (98), Milano (94)" --
// which is exactly the aggregate that scores 0% at every k when asked of passages
// (internal/documents/open.go). A document ingested from the bucket had none of it.
//
// A subprocess rather than an HTTP endpoint, deliberately. Aura's cockpit auth makes only
// GET /healthz public and restricts /readyz to loopback probes; a card endpoint would be a
// new unauthenticated surface on the same server for something that needs no store, no
// identity and no network. services/ingest/extract.py already shells out to `soffice` in
// this same image, so this follows a pattern that is there rather than adding a protocol.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/chetto1983/aura/internal/documents/filecard"
)

func main() {
	name := flag.String("name", "", "file name to describe the document as (defaults to the path's base name)")
	asJSON := flag.Bool("json", false, "emit {\"card\":...} instead of the rendered text")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: aura-filecard [-name NAME] [-json] PATH")
		os.Exit(2)
	}
	path := flag.Arg(0)
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-filecard: %v\n", err)
		os.Exit(1)
	}
	fileName := *name
	if fileName == "" {
		fileName = info.Name()
	}

	// A build error is NOT fatal, matching Service.writeCard: a file that could not be read
	// says so in its own card, because a card is how a document is FOUND and refusing to
	// describe a truncated zip would trade a real loss for a small one. The reason goes to
	// stderr so the caller can log it.
	card, buildErr := filecard.Build(filecard.Request{
		Path: path, FileName: fileName, SizeBytes: info.Size(),
	})
	if buildErr != nil {
		fmt.Fprintf(os.Stderr, "aura-filecard: %s: %v\n", fileName, buildErr)
	}
	rendered := card.Render()
	if *asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"card": rendered}); err != nil {
			fmt.Fprintf(os.Stderr, "aura-filecard: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Print(rendered)
}
