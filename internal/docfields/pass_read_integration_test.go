//go:build arcadedb_integration

package docfields

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

type passageQuery struct{}

// passageResponses records the rows of every passage read the store makes, through a proxy in
// front of ArcadeDB that the test's clients are pointed at.
type passageResponses struct {
	mu        sync.Mutex
	largest   int
	rowsByKey map[string]int
}

func recordPassageResponses(t *testing.T) *passageResponses {
	t.Helper()
	target, err := url.Parse(os.Getenv("ARCADEDB_URL"))
	if err != nil || target.Host == "" {
		t.Skip("ARCADEDB_URL not set")
	}
	seen := &passageResponses{rowsByKey: map[string]int{}}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Request.Context().Value(passageQuery{}) == nil {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		var out struct {
			Result []map[string]any `json:"result"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		seen.mu.Lock()
		defer seen.mu.Unlock()
		seen.largest = max(seen.largest, len(out.Result))
		for _, row := range out.Result {
			if _, ok := row["text"]; ok {
				seen.rowsByKey[str(row["source_key"])]++
			}
		}
		return nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.Header.Del("Accept-Encoding")
		if bytes.Contains(body, []byte("FROM Passage")) {
			r = r.WithContext(context.WithValue(r.Context(), passageQuery{}, true))
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ARCADEDB_URL", srv.URL)
	return seen
}

// A response of the passage read holds at most one page, whatever the batch holds: 200 manuals of
// 600 KB once came back as one response of about 120 MB, past the client's timeout, and the pass
// retried that batch for good. A document is not read past the text the pass matches.
func TestPassReadsPassagesAPageAtATime(t *testing.T) {
	seen := recordPassageResponses(t)
	_, ctx, _, c := indexedLibrary(t)
	const long = "chat/manuale.pdf"
	passage := func(i int) string { return fmt.Sprintf("p%03d %s", i, strings.Repeat("x", 4200)) }
	var script strings.Builder
	params := map[string]any{"long": long}
	for i := range passagePage + 44 {
		params[fmt.Sprintf("t%d", i)] = passage(i)
		fmt.Fprintf(&script, "INSERT INTO Passage SET passage_key = '%s#%d', source_key = :long, ordinal = %d, text = :t%d;\n", long, i, i, i)
	}
	if _, err := c.Script(ctx, script.String(), params); err != nil {
		t.Fatal(err)
	}
	indexDocument(t, c, "chat/a.txt", "2026-10-18 10:00:00.001", "testo a")
	indexDocument(t, c, "chat/b.txt", "2026-10-18 10:00:00.002", "testo b")
	if _, err := c.Command(ctx, "INSERT INTO IndexedDocument SET source_key = :long, file_name = :long, card = '', "+
		"header_pairs = [], indexed_at = '2026-10-18 10:00:00.000'", params); err != nil {
		t.Fatal(err)
	}
	docs, err := documentsDue(ctx, c, passBatch)
	if err != nil {
		t.Fatal(err)
	}
	texts := map[string]string{}
	for _, doc := range docs {
		texts[doc.SourceKey] = doc.Text
	}
	if texts["chat/a.txt"] != "testo a" || texts["chat/b.txt"] != "testo b" {
		t.Fatalf("the short documents read %q and %q", texts["chat/a.txt"], texts["chat/b.txt"])
	}
	if got := texts[long]; !strings.HasPrefix(got, passage(0)+"\n"+passage(1)+"\n") || len(got) < maxPassText {
		t.Fatalf("the long document read %d bytes, want its first %d in order", len(got), maxPassText)
	}
	if seen.largest > passagePage {
		t.Fatalf("one passage response held %d rows, want at most %d", seen.largest, passagePage)
	}
	if read := seen.rowsByKey[long]; read > passagePage {
		t.Fatalf("the long document's %d passages were read %d, past the page that reaches the pass's text", passagePage+44, read)
	}
}
