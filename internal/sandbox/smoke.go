package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PyodideSmokeScenario describes one offline runtime smoke program.
type PyodideSmokeScenario struct {
	Name        string
	Description string
	Marker      string
	Code        string
}

// PyodideSmokeScenarioResult captures one smoke scenario outcome.
type PyodideSmokeScenarioResult struct {
	Name      string
	OK        bool
	Stdout    string
	Stderr    string
	ExitCode  int
	ElapsedMs int
	Error     string
}

// PyodideSmokeReport captures the full offline Pyodide smoke run.
type PyodideSmokeReport struct {
	OK           bool
	Availability Availability
	Scenarios    []PyodideSmokeScenarioResult
	ElapsedMs    int
	Error        string
}

// PyodideSmokeScenarios returns the repeatable offline package smoke profile.
func PyodideSmokeScenarios() []PyodideSmokeScenario {
	return []PyodideSmokeScenario{
		{
			Name:        "arithmetic",
			Description: "basic Python execution",
			Marker:      "AURA_SMOKE arithmetic ok",
			Code: strings.TrimSpace(`
total = sum(range(101))
assert total == 5050, total
print("AURA_SMOKE arithmetic ok")
`),
		},
		{
			Name:        "data_imports",
			Description: "data science and utility imports",
			Marker:      "AURA_SMOKE data_imports ok",
			Code: strings.TrimSpace(`
import numpy as np
import pandas as pd
import scipy.stats as stats
import statsmodels.api as sm
import pyarrow as pa
import requests
import yaml
import dateutil.parser
import pytz
import tzdata
import regex
from rich.text import Text

arr = np.array([1, 2, 3, 4])
df = pd.DataFrame({"value": arr})
assert int(df["value"].sum()) == 10
assert round(float(stats.norm.cdf(0)), 2) == 0.5
assert sm.add_constant(arr).shape == (4, 2)
assert pa.array([1, 2, 3]).to_pylist() == [1, 2, 3]
assert yaml.safe_load("answer: 42")["answer"] == 42
assert dateutil.parser.parse("2026-05-04").year == 2026
assert pytz.timezone("UTC").zone == "UTC"
assert regex.search(r"\p{L}+", "Aura").group(0) == "Aura"
assert Text("ok").plain == "ok"
print("AURA_SMOKE data_imports ok")
`),
		},
		{
			Name:        "spreadsheet_read",
			Description: "offline XLSX read through python-calamine",
			Marker:      "AURA_SMOKE spreadsheet_read ok",
			Code: strings.TrimSpace(`
from zipfile import ZipFile, ZIP_DEFLATED
import xlrd
from python_calamine import load_workbook

files = {
    "[Content_Types].xml": """<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>""",
    "_rels/.rels": """<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>""",
    "xl/_rels/workbook.xml.rels": """<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>""",
    "xl/workbook.xml": """<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>""",
    "xl/worksheets/sheet1.xml": """<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>item</t></is></c><c r="B1" t="inlineStr"><is><t>value</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>alpha</t></is></c><c r="B2"><v>42</v></c></row></sheetData></worksheet>""",
}
with ZipFile("/tmp/smoke.xlsx", "w", ZIP_DEFLATED) as z:
    for name, body in files.items():
        z.writestr(name, body)
wb = load_workbook("/tmp/smoke.xlsx")
sheet = wb.get_sheet_by_name("Sheet1")
rows = sheet.to_python()
assert rows[1][0] == "alpha"
assert rows[1][1] == 42.0
assert xlrd.__version__
print("AURA_SMOKE spreadsheet_read ok")
`),
		},
		{
			Name:        "matplotlib_artifact",
			Description: "chart generation artifact in the Pyodide filesystem",
			Marker:      "AURA_SMOKE matplotlib_artifact ok",
			Code: strings.TrimSpace(`
import os
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

fig, ax = plt.subplots()
ax.plot([1, 2, 3], [1, 4, 9])
ax.set_title("Aura smoke")
fig.savefig("/tmp/plot.png", format="png")
plt.close(fig)
assert os.path.getsize("/tmp/plot.png") > 1000
print("AURA_SMOKE matplotlib_artifact ok")
`),
		},
		{
			Name:        "pdf_text_extraction",
			Description: "PDF text creation/extraction plus HTML parsers",
			Marker:      "AURA_SMOKE pdf_text_extraction ok",
			Code: strings.TrimSpace(`
import fitz
import lxml
import html5lib
from bs4 import BeautifulSoup

marker = "PDF smoke marker"
doc = fitz.open()
page = doc.new_page()
page.insert_text((72, 72), marker)
pdf_bytes = doc.tobytes()
doc.close()

read_doc = fitz.open(stream=pdf_bytes, filetype="pdf")
text = read_doc[0].get_text()
read_doc.close()
assert marker in text
soup = BeautifulSoup("<html><body><p>PDF smoke marker</p></body></html>", "html5lib")
assert soup.find("p").text == marker
assert lxml
print("AURA_SMOKE pdf_text_extraction ok")
`),
		},
	}
}

// RunPyodideSmoke validates that a runtime can execute Aura's offline package profile.
func RunPyodideSmoke(ctx context.Context, rt Runtime) PyodideSmokeReport {
	start := time.Now()
	report := PyodideSmokeReport{OK: true}
	if rt == nil {
		report.OK = false
		report.Availability = Availability{Available: false, Kind: RuntimeKindUnavailable, Detail: "sandbox runtime unavailable"}
		report.Error = report.Availability.Detail
		return report
	}

	report.Availability = normalizeAvailability(rt.Kind(), rt.CheckAvailability())
	if !report.Availability.Available {
		report.OK = false
		report.Error = report.Availability.Detail
		return report
	}

	for _, scenario := range PyodideSmokeScenarios() {
		result := PyodideSmokeScenarioResult{Name: scenario.Name}
		runResult, err := rt.Execute(ctx, scenario.Code, false)
		if err != nil {
			result.Error = err.Error()
		} else if runResult == nil {
			result.Error = "runtime returned nil result"
		} else {
			result.Stdout = runResult.Stdout
			result.Stderr = runResult.Stderr
			result.ExitCode = runResult.ExitCode
			result.ElapsedMs = runResult.ElapsedMs
			result.OK = runResult.OK && strings.Contains(runResult.Stdout, scenario.Marker)
			if !runResult.OK {
				result.Error = fmt.Sprintf("runtime exit_code=%d", runResult.ExitCode)
				if strings.TrimSpace(runResult.Stderr) != "" {
					result.Error += ": " + strings.TrimSpace(runResult.Stderr)
				}
			} else if !strings.Contains(runResult.Stdout, scenario.Marker) {
				result.Error = fmt.Sprintf("missing marker %q", scenario.Marker)
			}
		}
		if !result.OK {
			report.OK = false
		}
		report.Scenarios = append(report.Scenarios, result)
	}
	report.ElapsedMs = int(time.Since(start).Milliseconds())
	return report
}
