package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aura/aura/internal/skills"
)

// stubSkillsSh emulates skills.sh by returning a hand-crafted blob the
// existing CatalogClient regex picks up. The regex looks for fragments
// of the form
//
//	"source":"…","skillId":"…","name":"…","installs":N
//
// embedded anywhere in the body. We render two such tuples here so the
// test can verify both the JSON shape and the install_command string.
func stubSkillsSh(t *testing.T) *httptest.Server {
	t.Helper()
	body := `{"data":[
		{"source":"alpha/repo","skillId":"a","name":"Alpha","installs":42},
		{"source":"beta/repo","skillId":"b","name":"Beta","installs":7}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSkillsCatalog_PassthroughHappy(t *testing.T) {
	srv := stubSkillsSh(t)
	router := NewRouter(Deps{SkillsCatalog: skills.NewCatalogClient(srv.URL)})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/catalog", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []SkillCatalogItem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 items, got %d (%s)", len(got), rr.Body)
	}
	first := got[0]
	if first.Source != "alpha/repo" || first.SkillID != "a" || first.Name != "Alpha" || first.Installs != 42 {
		t.Errorf("first: %+v", first)
	}
	if !strings.Contains(first.InstallCommand, "npx skills add") {
		t.Errorf("install_command = %q", first.InstallCommand)
	}
}

func TestSkillsCatalog_QueryFilters(t *testing.T) {
	srv := stubSkillsSh(t)
	router := NewRouter(Deps{SkillsCatalog: skills.NewCatalogClient(srv.URL)})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/catalog?q=alpha", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []SkillCatalogItem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SkillID != "a" {
		t.Fatalf("expected only alpha, got %+v", got)
	}
}

func TestSkillsCatalog_NilClientReturnsEmpty(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/catalog", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("want [], got %q", rr.Body.String())
	}
}

func TestSkillsCatalog_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	router := NewRouter(Deps{SkillsCatalog: skills.NewCatalogClient(srv.URL)})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/skills/catalog", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}
