package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeInstaller and fakeDeleter satisfy the api.SkillInstaller /
// SkillDeleter interfaces with deterministic behavior so we can drive
// the handlers without spawning npx.

type fakeInstaller struct {
	called  bool
	source  string
	skillID string
	out     string
	err     error
}

func (f *fakeInstaller) Install(_ context.Context, source, skillID string) (string, error) {
	f.called = true
	f.source = source
	f.skillID = skillID
	return f.out, f.err
}

type fakeDeleter struct {
	called bool
	name   string
	err    error
}

func (f *fakeDeleter) Delete(name string) error {
	f.called = true
	f.name = name
	return f.err
}

func newSkillsWriteRouter(t *testing.T, admin bool, installer SkillInstaller, deleter SkillDeleter) http.Handler {
	t.Helper()
	return NewRouter(Deps{
		SkillsAdmin:     admin,
		SkillsInstaller: installer,
		SkillsDeleter:   deleter,
	})
}

func postJSON(t *testing.T, router http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader = http.NoBody
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest("POST", path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestSkillInstall_RejectsWhenAdminOff(t *testing.T) {
	inst := &fakeInstaller{}
	router := newSkillsWriteRouter(t, false, inst, nil)
	rr := postJSON(t, router, "/skills/install", map[string]string{"source": "user/repo"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if inst.called {
		t.Fatal("installer should not have been called")
	}
}

func TestSkillInstall_RejectsWhenInstallerNil(t *testing.T) {
	router := newSkillsWriteRouter(t, true, nil, nil)
	rr := postJSON(t, router, "/skills/install", map[string]string{"source": "user/repo"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillInstall_RejectsEmptySource(t *testing.T) {
	router := newSkillsWriteRouter(t, true, &fakeInstaller{}, nil)
	rr := postJSON(t, router, "/skills/install", map[string]string{"source": ""})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillInstall_RejectsBadSource(t *testing.T) {
	cases := []string{
		"with space",
		"semi;colon",
		"../escape",
		"path/with/../traversal",
		strings.Repeat("a", 201),
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			router := newSkillsWriteRouter(t, true, &fakeInstaller{}, nil)
			rr := postJSON(t, router, "/skills/install", map[string]string{"source": src})
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("source %q: status %d, body %s", src, rr.Code, rr.Body)
			}
		})
	}
}

func TestSkillInstall_RejectsBadSkillID(t *testing.T) {
	router := newSkillsWriteRouter(t, true, &fakeInstaller{}, nil)
	rr := postJSON(t, router, "/skills/install", map[string]any{
		"source":   "user/repo",
		"skill_id": "bad id with spaces",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillInstall_HappyPath(t *testing.T) {
	inst := &fakeInstaller{out: "added skill 'foo'\n"}
	router := newSkillsWriteRouter(t, true, inst, nil)
	rr := postJSON(t, router, "/skills/install", map[string]any{
		"source":   "user/repo",
		"skill_id": "foo",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if !inst.called || inst.source != "user/repo" || inst.skillID != "foo" {
		t.Fatalf("installer state: %+v", inst)
	}
	var got SkillInstallResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !strings.Contains(got.Output, "added skill") {
		t.Fatalf("response: %+v", got)
	}
}

func TestSkillInstall_FailureSurfacesOutput(t *testing.T) {
	inst := &fakeInstaller{out: "npm ERR! 404\n", err: errors.New("npx exited with code 1")}
	router := newSkillsWriteRouter(t, true, inst, nil)
	rr := postJSON(t, router, "/skills/install", map[string]string{"source": "missing/repo"})
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got SkillInstallResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.OK || !strings.Contains(got.Output, "npm ERR") || got.Error == "" {
		t.Fatalf("response: %+v", got)
	}
}

func TestSkillInstall_TruncatesLongOutput(t *testing.T) {
	huge := strings.Repeat("x", 4096)
	inst := &fakeInstaller{out: huge}
	router := newSkillsWriteRouter(t, true, inst, nil)
	rr := postJSON(t, router, "/skills/install", map[string]string{"source": "user/repo"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got SkillInstallResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.Output, "[truncated]") {
		t.Fatalf("expected truncation marker, got tail %q", got.Output[len(got.Output)-32:])
	}
}

func TestSkillDelete_RejectsWhenAdminOff(t *testing.T) {
	del := &fakeDeleter{}
	router := newSkillsWriteRouter(t, false, nil, del)
	rr := postJSON(t, router, "/skills/foo/delete", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if del.called {
		t.Fatal("deleter should not have been called")
	}
}

func TestSkillDelete_RejectsBadName(t *testing.T) {
	router := newSkillsWriteRouter(t, true, nil, &fakeDeleter{})
	rr := postJSON(t, router, "/skills/has%20space/delete", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillDelete_NotFound(t *testing.T) {
	router := newSkillsWriteRouter(t, true, nil, &fakeDeleter{err: ErrSkillNotFound})
	rr := postJSON(t, router, "/skills/missing/delete", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillDelete_HappyPath(t *testing.T) {
	del := &fakeDeleter{}
	router := newSkillsWriteRouter(t, true, nil, del)
	rr := postJSON(t, router, "/skills/foo/delete", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if !del.called || del.name != "foo" {
		t.Fatalf("deleter state: %+v", del)
	}
	var got SkillDeleteResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Name != "foo" {
		t.Fatalf("response: %+v", got)
	}
}

func TestSkillDelete_GenericError(t *testing.T) {
	del := &fakeDeleter{err: fmt.Errorf("disk full")}
	router := newSkillsWriteRouter(t, true, nil, del)
	rr := postJSON(t, router, "/skills/foo/delete", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}
