package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/skills"
)

// newSkillsRouter spins up a router with a real skills.Loader rooted at a
// temp dir. Other Deps are zero-value because the skill endpoints don't
// touch them.
func newSkillsRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	return NewRouter(Deps{Skills: skills.NewLoader(dir)}), dir
}

func writeSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsList_Empty(t *testing.T) {
	router, _ := newSkillsRouter(t)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []SkillSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty list, got %+v", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(rr.Body.String()), "[") {
		t.Errorf("expected JSON array, got %q", rr.Body.String())
	}
}

func TestSkillsList_NilLoader(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("want [], got %q", rr.Body.String())
	}
}

func TestSkillsList_Returns(t *testing.T) {
	router, dir := newSkillsRouter(t)
	writeSkill(t, dir, "alpha", "first", "alpha body")
	writeSkill(t, dir, "bravo", "second", "bravo body")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []SkillSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d skills, want 2", len(got))
	}
	// Loader sorts by name; alpha before bravo.
	if got[0].Name != "alpha" || got[1].Name != "bravo" {
		t.Errorf("unexpected order: %+v", got)
	}
	if got[0].Description != "first" {
		t.Errorf("description: %q", got[0].Description)
	}
}

func TestSkillGet_Found(t *testing.T) {
	router, dir := newSkillsRouter(t)
	writeSkill(t, dir, "alpha", "first", "alpha body content")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/alpha", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got SkillDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "alpha" || got.Description != "first" {
		t.Fatalf("unexpected detail: %+v", got)
	}
	if !strings.Contains(got.Content, "alpha body content") {
		t.Errorf("content missing body: %q", got.Content)
	}
	if got.Truncated {
		t.Errorf("expected not truncated for short skill")
	}
}

func TestSkillGet_NotFound(t *testing.T) {
	router, _ := newSkillsRouter(t)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillGet_RejectsBadName(t *testing.T) {
	router, _ := newSkillsRouter(t)
	rr := httptest.NewRecorder()
	// Path validation happens in the handler before LoadByName runs.
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/has%20space", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillGet_NilLoader(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/alpha", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestSkillGet_TruncatesLargeBody(t *testing.T) {
	router, dir := newSkillsRouter(t)
	huge := strings.Repeat("x", maxSkillBodyChars+1000)
	writeSkill(t, dir, "alpha", "first", huge)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/alpha", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got SkillDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Truncated {
		t.Errorf("expected truncated=true")
	}
	if len(got.Content) != maxSkillBodyChars {
		t.Errorf("content len = %d, want %d", len(got.Content), maxSkillBodyChars)
	}
}
