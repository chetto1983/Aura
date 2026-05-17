package release_test

import (
	"testing"

	"github.com/aura/aura/internal/release"
)

func TestVersionVarsAreDefined(t *testing.T) {
	if release.Version == "" {
		t.Error("Version must not be empty")
	}
	if release.Commit == "" {
		t.Error("Commit must not be empty")
	}
	if release.BuildDate == "" {
		t.Error("BuildDate must not be empty")
	}
}
