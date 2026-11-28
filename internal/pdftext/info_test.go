package pdftext

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/testenv"
)

func TestInspectCountsThePages(t *testing.T) {
	testenv.Executable(t, InfoTool)
	info, err := Inspect(context.Background(), []byte(pdfWithPages("one", "two", "three")))
	if err != nil || info.Pages != 3 || info.Encrypted {
		t.Fatalf("Inspect = %+v, %v; want 3 pages, not encrypted", info, err)
	}
}

func TestInspectRefusesWhatPdfinfoCannotOpen(t *testing.T) {
	testenv.Executable(t, InfoTool)
	if info, err := Inspect(context.Background(), []byte("%PDF-1.7 nothing behind the header")); err == nil {
		t.Fatalf("Inspect = %+v; want an error for a file with no pages to count", info)
	}
}

func TestParseInfo(t *testing.T) {
	info, err := parseInfo([]byte("Title:          Sheet: v2\nPages:          12\nEncrypted:      yes (print:yes copy:no)\n"))
	if err != nil || info.Pages != 12 || !info.Encrypted {
		t.Fatalf("parseInfo = %+v, %v", info, err)
	}
	for name, out := range map[string]string{
		"no page count":  "Title: x\nEncrypted: no\n",
		"bad page count": "Pages: many\n",
		"negative count": "Pages: -1\n",
	} {
		if _, err := parseInfo([]byte(out)); err == nil {
			t.Errorf("%s: parseInfo accepted %q", name, out)
		}
	}
}
