package objectstore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A Video Studio project is the editor's state, not a document anyone searches. The ingest
// sidecar can tell it apart by path alone -- its matcher sees keys, never the metadata that
// carries the name -- so the key keeps the Studio's suffix whole, while every other name still
// yields one short extension and never the name itself.
func TestAssetKeyKeepsTheStudioProjectSuffixWhole(t *testing.T) {
	for name, want := range map[string]string{
		"Reel di Aura.aura-video.json":       "chat/asset-2.aura-video.json",
		"REEL.AURA-VIDEO.JSON":               "chat/asset-2.aura-video.json",
		`C:\progetti\reel.aura-video.json`:   "chat/asset-2.aura-video.json",
		"reel.json":                          "chat/asset-2.json",
		"reel.aura-video.json.bak":           "chat/asset-2.bak",
		"reel-aura-video.json":               "chat/asset-2.json",
		"Contratto riservato.aura-video.pdf": "chat/asset-2.pdf",
	} {
		if got := AssetKey("asset-2", name, FolderChat); got != want {
			t.Errorf("AssetKey(%q) = %q, want %q", name, got, want)
		}
	}
}

// The suffix is spelled three times in three languages: here, in the cockpit that names the
// file, and in the ingest pattern that skips it. A drift in any one of them silently indexes
// every project saved after it, so the other two are read and compared, the way
// internal/embeddings/prefix_parity_test.go holds the ingest's document prefix to Go's.
func TestStudioProjectSuffixMatchesTheCockpitAndTheIngest(t *testing.T) {
	for _, spelling := range []struct {
		file    string
		pattern string
		dot     string
	}{
		{
			file:    filepath.Join("..", "..", "web", "src", "videoStudio", "projectStore.ts"),
			pattern: `(?m)^export const PROJECT_FILE_EXTENSION = '([^']*)';\s*$`,
			dot:     ".",
		},
		{
			file:    filepath.Join("..", "..", "services", "ingest", "source.py"),
			pattern: `(?m)^STUDIO_PROJECT_PATTERN = "\*\*/\*([^"]*)"\s*$`,
		},
	} {
		source, err := os.ReadFile(spelling.file)
		if err != nil {
			t.Fatalf("read %s: %v", spelling.file, err)
		}
		match := regexp.MustCompile(spelling.pattern).FindSubmatch(source)
		if match == nil {
			t.Fatalf("%s no longer declares the suffix on one line matching %s", spelling.file, spelling.pattern)
		}
		if got := spelling.dot + string(match[1]); got != StudioProjectSuffix {
			t.Errorf("%s spells the suffix %q; StudioProjectSuffix is %q", spelling.file, got, StudioProjectSuffix)
		}
	}
}
