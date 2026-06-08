package manager

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestCatalogIncludesTrustedRecipesAndCalendarFixture(t *testing.T) {
	catalog := BuiltInCatalog()
	names := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		names = append(names, entry.Name)
	}
	wantNames := []string{"calculator", "calendar", "mail", "whatsapp"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("catalog names = %#v, want %#v", names, wantNames)
	}

	calendar, ok := LookupCatalog("calendar")
	if !ok {
		t.Fatal("calendar recipe missing")
	}
	if calendar.Server.Trust.Class != mcp.TrustTrustedRecipe {
		t.Fatalf("calendar trust = %q, want %q", calendar.Server.Trust.Class, mcp.TrustTrustedRecipe)
	}
	if calendar.Server.Runtime.Kind != "local" {
		t.Fatalf("calendar runtime = %q, want local", calendar.Server.Runtime.Kind)
	}
	if !containsString(calendar.RequiredEnv, "AURA_CALENDAR_MODE=fixture") {
		t.Fatalf("calendar required env missing fixture mode: %#v", calendar.RequiredEnv)
	}
}

func TestLookupCatalogNotFound(t *testing.T) {
	entry, ok := LookupCatalog("does-not-exist")
	if ok {
		t.Fatalf("LookupCatalog(missing) ok = true, want false")
	}
	if !reflect.DeepEqual(entry, CatalogEntry{}) {
		t.Fatalf("LookupCatalog(missing) entry = %#v, want zero value", entry)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
