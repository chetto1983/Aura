package filecard

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const ooxmlParitySchema = "aura.filecard.ooxml-parity.v1"

type ooxmlParityManifest struct {
	SchemaID string `json:"schema_id"`
	Oracle   struct {
		Library string `json:"library"`
		Version string `json:"version"`
	} `json:"oracle"`
	Workbooks []ooxmlParityWorkbook `json:"workbooks"`
}

type ooxmlParityWorkbook struct {
	File   string             `json:"file"`
	SHA256 string             `json:"sha256"`
	Sheets []ooxmlParitySheet `json:"sheets"`
}

type ooxmlParitySheet struct {
	Name      string   `json:"name"`
	MaxRow    int64    `json:"max_row"`
	MaxColumn int      `json:"max_column"`
	HeaderRow int64    `json:"header_row"`
	Headers   []string `json:"headers"`
	NonEmpty  []int    `json:"non_empty"`
}

func TestOwnedWorkbookCorpusMatchesOpenpyxlOracle(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "..", "scripts", "fixtures", "document_retrieval_eval")
	manifest := readOOXMLParityManifest(t, filepath.Join(fixtureRoot, "ooxml_parity.json"))
	if manifest.SchemaID != ooxmlParitySchema {
		t.Fatalf("schema_id = %q, want %q", manifest.SchemaID, ooxmlParitySchema)
	}
	if manifest.Oracle.Library != "openpyxl" || manifest.Oracle.Version != "3.1.5" {
		t.Fatalf("oracle = %s %s, want openpyxl 3.1.5", manifest.Oracle.Library, manifest.Oracle.Version)
	}

	corpusRoot := filepath.Join(fixtureRoot, "corpus")
	paths, err := filepath.Glob(filepath.Join(corpusRoot, "*.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	actualFiles := make([]string, 0, len(paths))
	for _, path := range paths {
		actualFiles = append(actualFiles, filepath.Base(path))
	}
	expectedFiles := make([]string, 0, len(manifest.Workbooks))
	for _, workbook := range manifest.Workbooks {
		expectedFiles = append(expectedFiles, workbook.File)
	}
	slices.Sort(actualFiles)
	slices.Sort(expectedFiles)
	if !slices.Equal(actualFiles, expectedFiles) {
		t.Fatalf("XLSX corpus = %v, manifest = %v", actualFiles, expectedFiles)
	}

	for _, expected := range manifest.Workbooks {
		t.Run(expected.File, func(t *testing.T) {
			path := filepath.Join(corpusRoot, expected.File)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if digest := fmt.Sprintf("%x", sha256.Sum256(body)); digest != expected.SHA256 {
				t.Fatalf("sha256 = %s, oracle fixture = %s", digest, expected.SHA256)
			}

			card := build(t, path, expected.File)
			if len(card.Sheets) != len(expected.Sheets) {
				t.Fatalf("sheets = %d, oracle = %d", len(card.Sheets), len(expected.Sheets))
			}
			for index, want := range expected.Sheets {
				assertSheetParity(t, card.Sheets[index], want)
			}
		})
	}
}

func readOOXMLParityManifest(t *testing.T, path string) ooxmlParityManifest {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest ooxmlParityManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func assertSheetParity(t *testing.T, got Sheet, want ooxmlParitySheet) {
	t.Helper()
	if got.Name != want.Name {
		t.Fatalf("sheet name = %q, oracle = %q", got.Name, want.Name)
	}
	if rows := want.MaxRow - want.HeaderRow; got.Rows != rows {
		t.Fatalf("sheet %q rows = %d, oracle span = %d", got.Name, got.Rows, rows)
	}
	if len(got.Columns) != want.MaxColumn {
		t.Fatalf("sheet %q columns = %d, oracle = %d", got.Name, len(got.Columns), want.MaxColumn)
	}
	for index, column := range got.Columns {
		if column.Header != want.Headers[index] {
			t.Fatalf("sheet %q column %d header = %q, oracle = %q", got.Name, index+1, column.Header, want.Headers[index])
		}
		if column.NonEmpty != want.NonEmpty[index] {
			t.Fatalf("sheet %q column %d non-empty = %d, oracle = %d", got.Name, index+1, column.NonEmpty, want.NonEmpty[index])
		}
	}
}
