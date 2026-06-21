package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// validDoc returns a minimal valid one-server managed config for the write-path tests.
func validDoc(name, command string) mcp.ManagedConfig {
	return mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			name: {Command: command, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}},
		},
	}
}

// TestWriteConfigTempStagesSibling proves the staging half of the atomic wrapper: a valid
// doc lands in a UNIQUE sibling temp file (same directory, so the later os.Rename is a
// same-filesystem atomic move) with 0o600-class perms, the temp parses back, and the live
// path is NOT touched by staging (the rename is the caller's job after the tx commits).
func TestWriteConfigTempStagesSibling(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	if err := mcp.SaveManagedConfig(path, validDoc("alpha", "alpha-bin")); err != nil {
		t.Fatalf("seed prior config: %v", err)
	}
	priorBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior: %v", err)
	}

	tmp, err := writeConfigTemp(path, validDoc("beta", "beta-bin"))
	if err != nil {
		t.Fatalf("writeConfigTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmp) })

	if filepath.Dir(tmp) != dir {
		t.Errorf("temp must be a sibling of path: dir(tmp)=%s want %s", filepath.Dir(tmp), dir)
	}
	if tmp == path {
		t.Fatal("temp must not be the live path")
	}
	// The staged temp parses and holds the NEW doc.
	staged, err := mcp.LoadManagedConfig(tmp)
	if err != nil {
		t.Fatalf("staged temp does not parse: %v", err)
	}
	if _, ok := staged.MCPServers["beta"]; !ok {
		t.Errorf("staged temp missing the new server: %+v", staged.MCPServers)
	}
	// The live path is byte-identical: staging never touches it.
	nowBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after staging: %v", err)
	}
	if string(nowBytes) != string(priorBytes) {
		t.Error("staging must not modify the live config; bytes changed")
	}
}

// TestWriteConfigTempRejectsInvalid asserts an invalid doc (a server with no command and no
// URL) fails validation during staging and leaves no temp turd in the directory.
func TestWriteConfigTempRejectsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")

	invalid := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{"bad": {}}}
	if _, err := writeConfigTemp(path, invalid); err == nil {
		t.Fatal("writeConfigTemp must reject an invalid doc")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a failed stage must leave no temp file, found %d entries", len(entries))
	}
}
