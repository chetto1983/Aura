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
	return serveWriteLabelled(t, ops, method, target, body, "application/json")
}

func serveWriteLabelled(t *testing.T, ops FileOperations, method, target, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	server := &Server{}
	server.fileOps = ops
	mux := http.NewServeMux()
	server.registerFileRoutes(mux)
	req := withPrincipal(httptest.NewRequest(method, target, strings.NewReader(body)), fileAPIIdentityID)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// The CSRF floor every other write on this server stands on. A cross-origin form can post
// text/plain, multipart/form-data or application/x-www-form-urlencoded without a preflight;
// it cannot post application/json. So refusing a body that declares anything else is what
// makes the browser ask permission first, and these routes are the ones that create, rename,
// move, copy and delete the operator's files.
//
// The gate was dropped once because RestDataProvider.send sends no Content-Type at all --
// the subclass overrides Rest.sendRequest and loses its "application/json" default -- so the
// browser labelled every write text/plain and all five 400'd. The provider is labelled at
// the source now; this test is what stops the gate from being traded away a second time.
func TestFileWritesRefuseABodyThatIsNotLabelledJSON(t *testing.T) {
	writes := map[string]struct{ method, target, body string }{
		"create": {http.MethodPost, fileManagerBase + "/files/%2Fcontabilita", `{"name":"2027","type":"folder"}`},
		"rename": {http.MethodPut, fileManagerBase + "/files/%2Flistino.pdf", `{"operation":"rename","name":"x.pdf"}`},
		"bulk":   {http.MethodPut, fileManagerBase + "/files", `{"operation":"move","ids":["/a.txt"],"target":"/archivio"}`},
		"delete": {http.MethodDelete, fileManagerBase + "/files", `{"ids":["/a.txt"]}`},
	}
	// text/plain is the one a cross-origin form can actually send; the other two are the
	// rest of the simple-request set. An unparseable label must not fall through either.
	for _, contentType := range []string{"text/plain;charset=UTF-8", "application/x-www-form-urlencoded", "text/plain, text/plain"} {
		for name, write := range writes {
			t.Run(name+" "+contentType, func(t *testing.T) {
				ops := &fakeFileOps{}
				rec := serveWriteLabelled(t, ops, write.method, write.target, write.body, contentType)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", rec.Code)
				}
				if ops.call != "" {
					t.Fatalf("an unlabelled body reached the store as %q", ops.call)
				}
			})
		}
	}

	// The label the provider now sends must still get through, parameters and all.
	for name, write := range writes {
		t.Run(name+" application/json", func(t *testing.T) {
			ops := &fakeFileOps{}
			if got := serveWriteLabelled(t, ops, write.method, write.target, write.body,
				"application/json; charset=utf-8").Code; got != http.StatusOK {
				t.Fatalf("status = %d, want 200", got)
			}
			if ops.call == "" {
				t.Fatal("a labelled body never reached the store")
			}
		})
	}
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

// Opening renders user bytes on the cockpit's own origin, so what may render is an
// allowlist of types that cannot execute. The sandbox CSP that used to guard this was
// measured to break the thing it guarded: Chrome will not run its PDF viewer in an opaque
// origin, so every open answered "Download is starting" and left a blank tab.
func TestFileDirectRendersSafeTypesAndForcesTheRest(t *testing.T) {
	for mime, wantInline := range map[string]bool{
		"application/pdf":           true,
		"image/png":                 true,
		"text/plain; charset=utf-8": true,
		"audio/mpeg":                true,
		"video/mp4":                 true,
		// These execute. SVG especially: a script carrier wearing an image's clothes.
		"text/html":       false,
		"image/svg+xml":   false,
		"application/xml": false,
		"":                false,
	} {
		opener := &fakeFileOpener{body: "x", mime: mime}
		rec := serveFiles(t, fileServer(nil, opener), fileManagerBase+"/direct?id=%2Fnota")
		disposition := rec.Header().Get("Content-Disposition")
		if wantInline && !strings.HasPrefix(disposition, "inline") {
			t.Fatalf("%q = %q, want inline so the browser's own viewer opens it", mime, disposition)
		}
		if !wantInline && !strings.HasPrefix(disposition, "attachment") {
			t.Fatalf("%q = %q, want attachment: it can execute", mime, disposition)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%q lost nosniff", mime)
		}
		// No CSP on either path: the type allowlist is the control, and the directive
		// blocked the native viewers.
		if got := rec.Header().Get("Content-Security-Policy"); got != "" {
			t.Fatalf("%q carries a CSP again: %q", mime, got)
		}
	}

	// ?download=true is the component's "save it" and must never render, allowlisted or not.
	opener := &fakeFileOpener{body: "%PDF", mime: "application/pdf"}
	rec := serveFiles(t, fileServer(nil, opener), fileManagerBase+"/direct?id=%2Fa.pdf&download=true")
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("download Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("download Content-Disposition = %q", got)
	}
}
