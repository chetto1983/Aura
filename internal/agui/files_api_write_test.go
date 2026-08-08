package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeFileOps struct {
	call   string
	parent string
	id     string
	name   string
	kind   string
	target string
	ids    []string
	err    error
}

func (f *fakeFileOps) Create(_ context.Context, _, parent, name, kind string) (string, error) {
	f.call, f.parent, f.name, f.kind = "create", parent, name, kind
	return "/" + strings.TrimPrefix(parent+"/"+name, "/"), f.err
}

func (f *fakeFileOps) Rename(_ context.Context, _, id, name string) (string, error) {
	f.call, f.id, f.name = "rename", id, name
	return "/rinominato/" + name, f.err
}

func (f *fakeFileOps) Move(_ context.Context, _ string, ids []string, target string) ([]string, error) {
	f.call, f.ids, f.target = "move", ids, target
	return []string{target + "/a.txt"}, f.err
}

func (f *fakeFileOps) Copy(_ context.Context, _ string, ids []string, target string) ([]string, error) {
	f.call, f.ids, f.target = "copy", ids, target
	return []string{target + "/a.txt"}, f.err
}

func (f *fakeFileOps) Delete(_ context.Context, _ string, ids []string) error {
	f.call, f.ids = "delete", ids
	return f.err
}

func serveWrite(t *testing.T, ops FileOperations, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	server := &Server{}
	server.fileOps = ops
	mux := http.NewServeMux()
	server.registerFileRoutes(mux)
	req := withPrincipal(httptest.NewRequest(method, target, strings.NewReader(body)), fileAPIIdentityID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// These four payloads are the component's own, byte for byte what RestDataProvider sends.
// If the handler stops parsing one of them, the matching menu action silently stops working.
func TestFileWritesSpeakTheComponentsPayloads(t *testing.T) {
	for name, test := range map[string]struct {
		method, target, body string
		wantCall             string
		check                func(*testing.T, *fakeFileOps)
	}{
		"create": {
			http.MethodPost, fileManagerBase + "/files/%2Fcontabilita",
			`{"name":"2027","type":"folder"}`, "create",
			func(t *testing.T, ops *fakeFileOps) {
				if ops.parent != "/contabilita" || ops.name != "2027" || ops.kind != "folder" {
					t.Fatalf("create got %+v", ops)
				}
			},
		},
		"rename": {
			http.MethodPut, fileManagerBase + "/files/%2Flistino.pdf",
			`{"operation":"rename","name":"nuovo.pdf"}`, "rename",
			func(t *testing.T, ops *fakeFileOps) {
				if ops.id != "/listino.pdf" || ops.name != "nuovo.pdf" {
					t.Fatalf("rename got %+v", ops)
				}
			},
		},
		"move": {
			http.MethodPut, fileManagerBase + "/files",
			`{"operation":"move","ids":["/a.txt","/b.txt"],"target":"/archivio"}`, "move",
			func(t *testing.T, ops *fakeFileOps) {
				if len(ops.ids) != 2 || ops.target != "/archivio" {
					t.Fatalf("move got %+v", ops)
				}
			},
		},
		"copy": {
			http.MethodPut, fileManagerBase + "/files",
			`{"operation":"copy","ids":["/a.txt"],"target":"/archivio"}`, "copy",
			func(t *testing.T, ops *fakeFileOps) {
				if ops.target != "/archivio" {
					t.Fatalf("copy got %+v", ops)
				}
			},
		},
		"delete": {
			http.MethodDelete, fileManagerBase + "/files",
			`{"ids":["/a.txt"]}`, "delete",
			func(t *testing.T, ops *fakeFileOps) {
				if len(ops.ids) != 1 || ops.ids[0] != "/a.txt" {
					t.Fatalf("delete got %+v", ops)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ops := &fakeFileOps{}
			rec := serveWrite(t, ops, test.method, test.target, test.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			if ops.call != test.wantCall {
				t.Fatalf("call = %q, want %q", ops.call, test.wantCall)
			}
			test.check(t, ops)
		})
	}
}

// The component reads result.id back to reconcile the row it already drew. A create or
// rename that answered without one would leave the UI showing a name the store never used.
func TestFileWritesAnswerWithTheStoresOwnID(t *testing.T) {
	rec := serveWrite(t, &fakeFileOps{}, http.MethodPut,
		fileManagerBase+"/files/%2Flistino.pdf", `{"operation":"rename","name":"nuovo.pdf"}`)
	var single fileResult
	if err := json.Unmarshal(rec.Body.Bytes(), &single); err != nil {
		t.Fatal(err)
	}
	if single.Result == nil || single.Result.ID != "/rinominato/nuovo.pdf" || single.Result.Name != "nuovo.pdf" {
		t.Fatalf("result = %+v", single.Result)
	}

	rec = serveWrite(t, &fakeFileOps{}, http.MethodPut, fileManagerBase+"/files",
		`{"operation":"move","ids":["/a.txt"],"target":"/archivio"}`)
	var many fileResults
	if err := json.Unmarshal(rec.Body.Bytes(), &many); err != nil {
		t.Fatal(err)
	}
	if len(many.Result) != 1 || many.Result[0].ID != "/archivio/a.txt" {
		t.Fatalf("result = %+v", many.Result)
	}
}

func TestFileWritesRejectUnknownOperations(t *testing.T) {
	for _, test := range []struct{ target, body string }{
		{fileManagerBase + "/files/%2Fa.txt", `{"operation":"delete","name":"x"}`},
		{fileManagerBase + "/files", `{"operation":"teleport","ids":["/a.txt"],"target":"/x"}`},
	} {
		ops := &fakeFileOps{}
		if got := serveWrite(t, ops, http.MethodPut, test.target, test.body).Code; got != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", test.body, got)
		}
		if ops.call != "" {
			t.Fatalf("%s reached the store as %q", test.body, ops.call)
		}
	}

	// A delete with no ids is a client bug, and treating it as "delete nothing" would hide it.
	if got := serveWrite(t, &fakeFileOps{}, http.MethodDelete,
		fileManagerBase+"/files", `{"ids":[]}`).Code; got != http.StatusBadRequest {
		t.Fatalf("empty delete = %d, want 400", got)
	}
}

func TestFileWritesRefuseWithoutAProviderOrPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerFileRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, fileManagerBase+"/files",
		strings.NewReader(`{"ids":["/a.txt"]}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired delete = %d, want 503", rec.Code)
	}

	server := &Server{}
	server.fileOps = &fakeFileOps{}
	mux = http.NewServeMux()
	server.registerFileRoutes(mux)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, fileManagerBase+"/files",
		strings.NewReader(`{"ids":["/a.txt"]}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous delete = %d, want 401", rec.Code)
	}
}

// A store refusal must reach the component as an error envelope, not a silent success: the
// row is already on screen, so a swallowed failure leaves a file the user thinks exists.
func TestFileWritesSurfaceStoreFailures(t *testing.T) {
	ops := &fakeFileOps{err: errors.New("bucket unreachable")}
	rec := serveWrite(t, ops, http.MethodDelete, fileManagerBase+"/files", `{"ids":["/a.txt"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var envelope fileResults
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == "" {
		t.Fatalf("no error in the envelope: %s", rec.Body)
	}
}

// Opening renders user bytes on the cockpit's own origin, which is only safe because the
// response sandboxes itself. If this header goes, an uploaded .html runs with the operator's
// session — so the assertion is on the header, not on the disposition alone.
func TestFileDirectSandboxesAnInlineOpen(t *testing.T) {
	opener := &fakeFileOpener{body: "<h1>ciao</h1>", mime: "text/html"}
	rec := serveFiles(t, fileServer(nil, opener), fileManagerBase+"/direct?id=%2Fnota.html")
	// The sandbox directive is the whole control: an opaque origin, so no script runs and
	// nothing reaches the cockpit's session. It must not carry default-src 'none' with it --
	// that also blocks the document's own resources, so nothing renders.
	if got := rec.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("Content-Security-Policy = %q, want exactly \"sandbox\"", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff missing: %q", got)
	}

	// ?download=true is the component's "save it" and must never render.
	rec = serveFiles(t, fileServer(nil, opener), fileManagerBase+"/direct?id=%2Fnota.html&download=true")
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("download Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("download Content-Disposition = %q", got)
	}
}
