package mcp

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestViewCatalog_RoundTrip(t *testing.T) {
	catalog := NewViewCatalog()
	doc := ViewDocument{Server: "docs", ResourceURI: "ui://docs/a.html", HTML: "<html></html>"}
	if err := catalog.Put(doc); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := catalog.Get("docs", "ui://docs/a.html")
	if !ok || got.HTML != doc.HTML {
		t.Fatalf("Get = %#v, ok=%v", got, ok)
	}
}

// The key is the PAIR. Two servers may serve the same URI — `ui://` names a
// document inside a server, not on the network — and neither may see the other's.
func TestViewCatalog_KeyIsServerAndURI(t *testing.T) {
	catalog := NewViewCatalog()
	uri := "ui://app/a.html"
	if err := catalog.Put(ViewDocument{Server: "one", ResourceURI: uri, HTML: "<b>one</b>"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Put(ViewDocument{Server: "two", ResourceURI: uri, HTML: "<b>two</b>"}); err != nil {
		t.Fatal(err)
	}
	one, _ := catalog.Get("one", uri)
	two, _ := catalog.Get("two", uri)
	if one.HTML == two.HTML {
		t.Fatal("two servers sharing a URI must not share a document")
	}
	if _, ok := catalog.Get("three", uri); ok {
		t.Fatal("a server that catalogued nothing must miss")
	}
}

func TestViewCatalog_RejectsWhatMustNotReachABrowser(t *testing.T) {
	catalog := NewViewCatalog()
	for name, doc := range map[string]ViewDocument{
		"a non-ui scheme":  {Server: "s", ResourceURI: "https://evil.test/a.html", HTML: "<b>x</b>"},
		"an empty scheme":  {Server: "s", ResourceURI: "", HTML: "<b>x</b>"},
		"an empty body":    {Server: "s", ResourceURI: "ui://s/a.html", HTML: "   "},
		"an oversize body": {Server: "s", ResourceURI: "ui://s/a.html", HTML: strings.Repeat("x", MaxViewBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := catalog.Put(doc); err == nil {
				t.Fatal("must be rejected")
			}
			if _, ok := catalog.Get(doc.Server, doc.ResourceURI); ok {
				t.Fatal("a rejected document must not be stored")
			}
		})
	}
}

func TestViewCatalog_HasServerAndURIs(t *testing.T) {
	catalog := NewViewCatalog()
	for _, doc := range []ViewDocument{
		{Server: "b", ResourceURI: "ui://b/z.html", HTML: "<i>z</i>"},
		{Server: "a", ResourceURI: "ui://a/y.html", HTML: "<i>y</i>"},
	} {
		if err := catalog.Put(doc); err != nil {
			t.Fatal(err)
		}
	}
	if !catalog.HasServer("a") || catalog.HasServer("missing") {
		t.Fatal("HasServer must answer from what was catalogued")
	}
	want := []string{"a ui://a/y.html", "b ui://b/z.html"}
	if got := catalog.URIs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("URIs = %#v, want %#v (sorted)", got, want)
	}
}

// A nil catalog is the "this host renders nothing" case every mount path passes
// unconditionally. It must be inert, not a panic.
func TestViewCatalog_NilIsInert(t *testing.T) {
	var catalog *ViewCatalog
	if err := catalog.Put(ViewDocument{Server: "s", ResourceURI: "ui://s/a.html", HTML: "<b>x</b>"}); err != nil {
		t.Fatalf("a nil catalog must accept and drop, got %v", err)
	}
	if _, ok := catalog.Get("s", "ui://s/a.html"); ok {
		t.Fatal("a nil catalog must never hit")
	}
	if catalog.HasServer("s") || catalog.URIs() != nil {
		t.Fatal("a nil catalog must report nothing")
	}
}

// Mounts run concurrently and the HTTP layer reads while they do.
func TestViewCatalog_ConcurrentPutAndGet(t *testing.T) {
	catalog := NewViewCatalog()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = catalog.Put(ViewDocument{Server: "s", ResourceURI: "ui://s/a.html", HTML: "<b>x</b>"})
		}()
		go func() {
			defer wg.Done()
			catalog.Get("s", "ui://s/a.html")
			catalog.HasServer("s")
			_ = i
		}()
	}
	wg.Wait()
}
