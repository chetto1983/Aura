//go:build cot_eval

// skills_xlsx_verify_cot_eval_test.go carries the artifact ground-truth half of the
// xlsx North-Star E2E (split from skills_cot_eval_test.go per the 600-LOC cap):
// the HOST-side openpyxl read-back that proves the produced workbook exists, opens,
// and contains today's data (artifact-not-reply, D-35; host surface per #52/D-41).
package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// verifyXlsxArtifact is the artifact-not-reply ground truth (D-35, #52/D-41): it walks
// the HOST run workspace for the newest *.xlsx the run produced (mtime ≥ runStart, the
// spike-012a freshness discipline) and re-opens it via a HOST openpyxl read-back,
// asserting the workbook parses AND carries today's date. It NEVER inspects the model's
// prose — the file on disk is the truth. No sandbox, no docker: the loop ran on the host
// terminal, so its artifact is on the host filesystem.
func verifyXlsxArtifact(t *testing.T, exp *skillsExpect, workspace string, runStart time.Time, res *skillsResult) {
	t.Helper()

	newest, found := newestArtifact(t, workspace, exp.xlsxExt)
	if !found {
		res.notes = append(res.notes, "no "+exp.xlsxExt+" found under the host workspace "+workspace)
		return
	}
	res.xlsxPath = newest
	// Freshness: a .xlsx whose mtime predates the run start is a stale artifact from a
	// prior run, not THIS run's output (spike-012a run-start discipline).
	info, err := os.Stat(newest)
	if err != nil {
		res.notes = append(res.notes, "stat artifact "+newest+": "+err.Error())
		return
	}
	res.xlsxFresh = !info.ModTime().Before(runStart)
	// xlsxExists asserts the run PRODUCED the artifact — a stale .xlsx must not satisfy
	// the hard floor (spike-012a freshness discipline).
	res.xlsxExists = res.xlsxFresh
	if !res.xlsxFresh {
		res.notes = append(res.notes, "newest "+exp.xlsxExt+" is older than the run start (stale, not this run's output): "+newest)
	}

	openpyxlReadBack(t, exp, newest, res)
}

// newestArtifact walks the host workspace tree for files ending in ext and returns the
// one with the most recent mtime (the run's output). found is false when none exist.
func newestArtifact(t *testing.T, workspace, ext string) (newest string, found bool) {
	t.Helper()
	var newestMod time.Time
	_ = filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries; absence is handled by found=false
		}
		if !strings.HasSuffix(strings.ToLower(path), strings.ToLower(ext)) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if !found || info.ModTime().After(newestMod) {
			newest, newestMod, found = path, info.ModTime(), true
		}
		return nil
	})
	return newest, found
}

// openpyxlReadBack re-opens the artifact through the HOST python interpreter (python3,
// falling back to python) with openpyxl, scanning every cell's string form for ANY
// accepted encoding of today's date. It sets res.xlsxOpens / res.xlsxToday from the
// ground truth — never the prose. If no python is on the host PATH the verify FAILS with
// a clear message (no skip-as-green: the host gate requires a real interpreter).
func openpyxlReadBack(t *testing.T, exp *skillsExpect, artifact string, res *skillsResult) {
	t.Helper()
	py, ok := hostPython()
	if !ok {
		res.notes = append(res.notes, "no host python (python3/python) on PATH — cannot read back the .xlsx; verify FAILS (no skip-as-green)")
		t.Errorf("xlsx read-back: no host python interpreter found — install python3 with openpyxl on the host")
		return
	}

	today, formsPy := todayDateForms()
	const marker = "AURA-XLSX-VERIFIED"
	script := fmt.Sprintf(`
import sys
path = sys.argv[1]
try:
    import %s
    wb = %s.load_workbook(path, read_only=True, data_only=True)
except Exception as e:
    print('OPEN_FAIL=' + str(e)); sys.exit(0)
print('XLSX_OPENS')
dates = [%s]
found = False
for ws in wb.worksheets:
    for row in ws.iter_rows(values_only=True):
        for cell in row:
            if cell is not None and any(d in str(cell) for d in dates):
                found = True
                break
        if found: break
    if found: break
print('TODAY_FOUND' if found else 'TODAY_MISSING')
print('%s')
`, exp.readBackImport, exp.readBackImport, strings.Join(formsPy, ", "), marker)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, py, "-c", script, artifact).CombinedOutput()
	text := string(out)
	if err != nil && !strings.Contains(text, "XLSX_OPENS") && !strings.Contains(text, "OPEN_FAIL=") {
		res.notes = append(res.notes, "xlsx read-back exec error: "+err.Error()+" ("+oneLine(text)+")")
		t.Errorf("xlsx read-back exec: %v", err)
		return
	}
	res.xlsxOpens = strings.Contains(text, "XLSX_OPENS")
	res.xlsxToday = strings.Contains(text, "TODAY_FOUND")
	if !res.xlsxOpens {
		res.notes = append(res.notes, "xlsx did not open: "+oneLine(text))
	}
	if !res.xlsxToday {
		res.notes = append(res.notes, "xlsx missing today's date ("+today+")")
	}
}

// hostPython picks the host interpreter once: python3 preferred, python as a fallback.
func hostPython() (string, bool) {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	return "", false
}

// todayDateForms returns today's date in every encoding a live artifact legitimately
// uses (the scenario prompt is Italian, so the sheet writes 06/06/2026 or the spelled-out
// month at least as often as ISO — the assertion's INTENT is "contains today's data") and
// the matching python single-quoted literals for the read-back scan.
func todayDateForms() (joined string, formsPy []string) {
	now := time.Now()
	itMonths := []string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
		"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}
	forms := []string{
		now.Format("2006-01-02"), // ISO
		now.Format("02/01/2006"), // IT dd/mm/yyyy
		fmt.Sprintf("%d/%d/%d", now.Day(), int(now.Month()), now.Year()), // IT d/m/yyyy
		now.Format("02-01-2006"), // IT dd-mm-yyyy
		fmt.Sprintf("%d %s %d", now.Day(), itMonths[now.Month()-1], now.Year()), // IT spelled-out
		now.Format("January 2, 2006"),                                           // EN spelled-out
	}
	formsPy = make([]string, len(forms))
	for i, f := range forms {
		formsPy[i] = "'" + strings.ReplaceAll(f, "'", "") + "'"
	}
	return strings.Join(forms, " | "), formsPy
}
