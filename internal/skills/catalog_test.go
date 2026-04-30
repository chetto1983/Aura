package skills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseCatalogItems(t *testing.T) {
	raw := `self.__next_f.push([1,"{\"source\":\"vercel-labs/skills\",\"skillId\":\"find-skills\",\"name\":\"find-skills\",\"installs\":1300000},{\"source\":\"anthropics/skills\",\"skillId\":\"frontend-design\",\"name\":\"frontend-design\",\"installs\":353600}"])`
	items := parseCatalogItems(raw)
	if len(items) != 2 {
		t.Fatalf("parseCatalogItems() length = %d, want 2", len(items))
	}
	if items[0].Source != "vercel-labs/skills" || items[0].SkillID != "find-skills" || items[0].Installs != 1300000 {
		t.Fatalf("first item = %#v", items[0])
	}
}

func TestCatalogClientSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{\"source\":\"vercel-labs/agent-skills\",\"skillId\":\"vercel-react-best-practices\",\"name\":\"vercel-react-best-practices\",\"installs\":361000}`)
		fmt.Fprint(w, `{\"source\":\"anthropics/skills\",\"skillId\":\"frontend-design\",\"name\":\"frontend-design\",\"installs\":353600}`)
	}))
	defer srv.Close()

	client := NewCatalogClient(srv.URL)
	items, err := client.Search(context.Background(), "react", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Search() length = %d, want 1", len(items))
	}
	if items[0].SkillID != "vercel-react-best-practices" {
		t.Fatalf("Search() first = %#v", items[0])
	}
	if cmd := items[0].InstallCommand(); !strings.Contains(cmd, "npx skills add vercel-labs/agent-skills --skill vercel-react-best-practices") {
		t.Fatalf("InstallCommand() = %q", cmd)
	}
}
