package skills

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeArtifactSkillMaterializesItsToolchain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatal(err)
	}
	skill, ok := NewLoader(Config{Roots: []string{dir}}).Get("web-artifacts-builder")
	if !ok || !IsBuiltin("web-artifacts-builder") || skill.Always {
		t.Fatal("artifact skill must be native, discoverable and loaded on demand")
	}
	for _, resource := range []string{"scripts/init-artifact.sh", "scripts/bundle-artifact.sh", "scripts/verify-artifact.py", "LICENSE.txt"} {
		content, err := os.ReadFile(filepath.Join(dir, "web-artifacts-builder", resource))
		if err != nil || len(content) == 0 {
			t.Fatalf("missing native resource %s: %v", resource, err)
		}
	}
	archive, err := os.Open(filepath.Join(dir, "web-artifacts-builder/scripts/shadcn-components.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	gz, err := gzip.NewReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	files := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			files++
		}
	}
	if files < 40 {
		t.Fatalf("component archive is incomplete: %d files", files)
	}
}
