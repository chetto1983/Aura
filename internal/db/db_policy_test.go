package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionSQLiteOpensGoThroughSharedDBPackage(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			switch name {
			case ".git", "data", "wiki", "web", "runtime", "logs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		clean := filepath.ToSlash(path)
		if clean == "internal/db/db.go" || strings.HasSuffix(clean, "/internal/db/db.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(data)
		if strings.Contains(source, `sql.Open("sqlite"`) || strings.Contains(source, "sql.Open(`sqlite`") {
			offenders = append(offenders, clean)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production sources: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("production files must use internal/db.Open instead of sql.Open(\"sqlite\"): %s", strings.Join(offenders, ", "))
	}
}
