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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/skills"
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
	// onDelete, when non-nil, runs on the success path so a test can
	// remove the matching SKILL.md from disk to mimic what the real
	// deleter would do.
	onDelete func(name string)
}

func (f *fakeDeleter) Delete(name string) error {
	f.called = true
	f.name = name
	if f.err != nil {
		return f.err
	}
	if f.onDelete != nil {
		f.onDelete(name)
	}
	return nil
}

func newSkillsWriteRouter(t *testing.T, admin bool, installer SkillInstaller, deleter SkillDeleter) http.Handler {
	t.Helper()
	return NewRouter(Deps{
		SkillsAdmin:     admin,
		SkillsInstaller: installer,
		SkillsDeleter:   deleter,
	})
}

// newSkillsWriteRouterWithLoader constructs a router that wires a real
// *skills.Loader into Deps. The cache-invalidation tests need this so
// they can prime LoadAll, mutate disk through the handler, and assert
// the next LoadAll sees the change inside the cacheTTL window.
func newSkillsWriteRouterWithLoader(t *testing.T, admin bool, installer SkillInstaller, deleter SkillDeleter, loader *skills.Loader) http.Handler {
	t.Helper()
	return NewRouter(Deps{
		SkillsAdmin:     admin,
		SkillsInstaller: installer,
		SkillsDeleter:   deleter,
		Skills:          loader,
	})
}

// writeSkillFile writes a minimal valid SKILL.md under root/<name>/ so
// the loader picks it up. Mirrors writeSkill in skills_test.go but kept
// local to avoid coupling the two test files.
func writeSkillFile(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsSkill(skills []skills.Skill, name string) bool {
	for _, s := range skills {
		if s.Name == name {
			return true
		}
	}
	return false
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

// TestSkillInstall_InvalidatesLoaderCache pins the contract that the
// /skills/install handler clears the LoadAll cache on success. Without
// the Invalidate call the second LoadAll returns the cached pre-install
// snapshot for up to cacheTTL (1s) and the prompt-side manifest stays
// stale; with it, the new skill is visible immediately.
func TestSkillInstall_InvalidatesLoaderCache(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "alpha", "first", "alpha body")
	loader := skills.NewLoader(dir)

	// Prime the cache: pull the current state into LoadAll's cached field.
	before, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("prime LoadAll: %v", err)
	}
	if len(before) != 1 || before[0].Name != "alpha" {
		t.Fatalf("unexpected prime state: %+v", before)
	}

	// The fake installer doesn't actually write to disk, so we do it
	// here just before driving the handler. From the loader's point of
	// view this models the post-install on-disk state.
	writeSkillFile(t, dir, "bravo", "second", "bravo body")

	inst := &fakeInstaller{out: "added skill 'bravo'\n"}
	router := newSkillsWriteRouterWithLoader(t, true, inst, nil, loader)
	rr := postJSON(t, router, "/skills/install", map[string]any{
		"source":   "user/repo",
		"skill_id": "bravo",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if !inst.called {
		t.Fatal("installer not invoked")
	}

	after, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("post LoadAll: %v", err)
	}
	if !containsSkill(after, "bravo") {
		t.Fatalf("loader cache still stale, expected bravo in %+v", after)
	}
}

// TestSkillDelete_InvalidatesLoaderCache is the symmetric assertion for
// the delete handler. The fake deleter receives an onDelete callback
// that performs the disk removal; after the handler returns, LoadAll
// must reflect the removal without waiting on cacheTTL.
func TestSkillDelete_InvalidatesLoaderCache(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "alpha", "first", "alpha body")
	writeSkillFile(t, dir, "bravo", "second", "bravo body")
	loader := skills.NewLoader(dir)

	before, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("prime LoadAll: %v", err)
	}
	if !containsSkill(before, "bravo") {
		t.Fatalf("expected bravo in primed cache: %+v", before)
	}

	del := &fakeDeleter{
		onDelete: func(name string) {
			if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
				t.Errorf("removeAll %q: %v", name, err)
			}
		},
	}
	router := newSkillsWriteRouterWithLoader(t, true, nil, del, loader)
	rr := postJSON(t, router, "/skills/bravo/delete", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if !del.called || del.name != "bravo" {
		t.Fatalf("deleter state: %+v", del)
	}

	after, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("post LoadAll: %v", err)
	}
	if containsSkill(after, "bravo") {
		t.Fatalf("loader cache still stale, bravo should be gone: %+v", after)
	}
}
