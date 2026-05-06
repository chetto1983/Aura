package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aura/aura/internal/source"
)

const (
	defaultPyodideRuntimeDir        = "./runtime/pyodide"
	defaultPyodideRunnerTimeout     = 15 * time.Second
	defaultPyodideRunnerOutputBytes = 1 << 20
	defaultPyodideResultOutputBytes = 64 * 1024
	defaultPyodideOutputDir         = "/tmp/aura_out"
	maxPyodideArtifacts             = 10
	maxPyodideArtifactBytes         = 5 << 20
)

const trustedXLSXExtractorCode = `
import json
from pathlib import Path
import pandas as pd

input_path = Path("workbook.xlsx")
output_dir = Path("/tmp/aura_out")
output_dir.mkdir(parents=True, exist_ok=True)

book = pd.ExcelFile(input_path, engine="calamine")
sections = []
rows_total = 0
warnings = []

def clean_cell(value):
    text = "" if value is None else str(value)
    return text.replace("|", "\\|").replace("\n", " ").strip()

def markdown_table(df):
    df = df.fillna("")
    headers = [clean_cell(c) for c in list(df.columns)]
    lines = ["| " + " | ".join(headers) + " |"]
    lines.append("| " + " | ".join(["---"] * len(headers)) + " |")
    for _, row in df.head(200).iterrows():
        lines.append("| " + " | ".join(clean_cell(row[c]) for c in df.columns) + " |")
    return "\n".join(lines)

for sheet in book.sheet_names[:10]:
    df = book.parse(sheet)
    rows_total += int(len(df.index))
    sections.append("## Sheet: " + str(sheet))
    sections.append(markdown_table(df))

if len(book.sheet_names) > 10:
    warnings.append("workbook truncated to first 10 sheets")
if rows_total > 200:
    warnings.append("large workbook rendered with per-sheet row limits")

markdown = "\n\n".join(sections).strip() + "\n"
(output_dir / "extract.md").write_text(markdown, encoding="utf-8")
(output_dir / "extract.json").write_text(json.dumps({
    "extractor_name": "pyodide_xlsx",
    "extractor_version": "pyodide_xlsx_v1",
    "text_bytes": len(markdown.encode("utf-8")),
    "sheet_count": len(book.sheet_names),
    "row_count": rows_total,
    "warnings": warnings
}), encoding="utf-8")
print("xlsx extraction complete")
`

const trustedDOCXExtractorCode = `
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET
import zipfile

input_path = Path("document.docx")
output_dir = Path("/tmp/aura_out")
output_dir.mkdir(parents=True, exist_ok=True)
warnings = []

MAX_DOCX_ENTRIES = 512
MAX_DOCX_TOTAL_UNCOMPRESSED_BYTES = 20 * 1024 * 1024
MAX_DOCX_XML_PART_BYTES = 2 * 1024 * 1024
MAX_DOCX_COMPRESSION_RATIO = 100
MAX_DOCX_PARAGRAPHS = 5000
MAX_DOCX_TEXT_BYTES = 512 * 1024

WORD_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
TEXT_TAG = "{" + WORD_NS + "}t"
PARA_TAG = "{" + WORD_NS + "}p"
TAB_TAG = "{" + WORD_NS + "}tab"
BR_TAG = "{" + WORD_NS + "}br"
CR_TAG = "{" + WORD_NS + "}cr"

def clean_text(text):
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    lines = [re.sub(r"[ \t]+", " ", line).strip() for line in text.split("\n")]
    return "\n".join(line for line in lines if line).strip()

def paragraph_text(paragraph):
    parts = []
    for elem in paragraph.iter():
        if elem.tag == TEXT_TAG and elem.text:
            parts.append(elem.text)
        elif elem.tag == TAB_TAG:
            parts.append("\t")
        elif elem.tag in (BR_TAG, CR_TAG):
            parts.append("\n")
    return clean_text("".join(parts))

def part_paragraphs(zf, name):
    try:
        info = zf.getinfo(name)
    except KeyError:
        return []
    if info.file_size > MAX_DOCX_XML_PART_BYTES:
        warnings.append(f"skipped oversized XML part {name}")
        return []
    if info.compress_size > 0 and info.file_size > info.compress_size * MAX_DOCX_COMPRESSION_RATIO:
        warnings.append(f"skipped suspiciously compressed XML part {name}")
        return []
    raw = zf.read(info)
    try:
        root = ET.fromstring(raw)
    except ET.ParseError as exc:
        warnings.append(f"skipped malformed XML part {name}: {exc}")
        return []
    paragraphs = []
    for paragraph in root.iter(PARA_TAG):
        if len(paragraphs) >= MAX_DOCX_PARAGRAPHS:
            warnings.append(f"truncated paragraph extraction in {name}")
            break
        text = paragraph_text(paragraph)
        if text:
            paragraphs.append(text)
    return paragraphs

sections = []
extracted_text_bytes = 0
extracted_paragraphs = 0
truncated = False

def append_block(text, counts_as_paragraph=True):
    global extracted_text_bytes, extracted_paragraphs, truncated
    if counts_as_paragraph and extracted_paragraphs >= MAX_DOCX_PARAGRAPHS:
        if not truncated:
            warnings.append("document truncated at paragraph limit")
        truncated = True
        return False
    projected = extracted_text_bytes + len((text + "\n\n").encode("utf-8"))
    if projected > MAX_DOCX_TEXT_BYTES:
        if not truncated:
            warnings.append("document truncated at text byte limit")
        truncated = True
        return False
    sections.append(text)
    extracted_text_bytes = projected
    if counts_as_paragraph:
        extracted_paragraphs += 1
    return True

def append_part(label, paragraphs):
    if not paragraphs:
        return
    if label and not append_block(label, False):
        return
    for text in paragraphs:
        if not append_block(text):
            return

with zipfile.ZipFile(input_path) as zf:
    infos = zf.infolist()
    if len(infos) > MAX_DOCX_ENTRIES:
        raise RuntimeError("DOCX has too many ZIP entries")
    total_uncompressed = sum(info.file_size for info in infos)
    if total_uncompressed > MAX_DOCX_TOTAL_UNCOMPRESSED_BYTES:
        raise RuntimeError("DOCX uncompressed size exceeds limit")
    names = set(zf.namelist())
    if "word/document.xml" not in names:
        raise RuntimeError("DOCX missing word/document.xml")

    header_parts = sorted(name for name in names if re.match(r"word/header\d+\.xml$", name))
    footer_parts = sorted(name for name in names if re.match(r"word/footer\d+\.xml$", name))

    for name in header_parts:
        append_part("## Header", part_paragraphs(zf, name))

    append_part("", part_paragraphs(zf, "word/document.xml"))

    for name in footer_parts:
        append_part("## Footer", part_paragraphs(zf, name))

markdown = "\n\n".join(sections).strip()
if not markdown:
    warnings.append("document contained no extractable text")
markdown += "\n"

(output_dir / "extract.md").write_text(markdown, encoding="utf-8")
(output_dir / "extract.json").write_text(json.dumps({
    "extractor_name": "pyodide_docx",
    "extractor_version": "pyodide_docx_v1",
    "text_bytes": len(markdown.encode("utf-8")),
    "page_count": 0,
    "warnings": warnings
}), encoding="utf-8")
print("docx extraction complete")
`

// PyodideRunnerConfig controls the bundled Pyodide runner adapter.
type PyodideRunnerConfig struct {
	RuntimeDir            string
	RunnerPath            string
	RunnerArgs            []string
	Timeout               time.Duration
	Environment           []string
	MaxProcessOutputBytes int64
	MaxResultOutputBytes  int
}

// PyodideRunner executes Python through Aura's bundled Pyodide runner process.
type PyodideRunner struct {
	runtimeDir            string
	runnerPath            string
	runnerArgs            []string
	timeout               time.Duration
	environment           []string
	maxProcessOutputBytes int64
	maxResultOutputBytes  int
}

type pyodideRunnerRequest struct {
	Code                string   `json:"code"`
	TimeoutMS           int      `json:"timeout_ms"`
	AllowNetwork        bool     `json:"allow_network"`
	Packages            []string `json:"packages"`
	InputFiles          []string `json:"input_files"`
	OutputFileAllowlist []string `json:"output_file_allowlist"`
}

type pyodideRunnerResponse struct {
	OK        bool                      `json:"ok"`
	Stdout    string                    `json:"stdout"`
	Stderr    string                    `json:"stderr"`
	ExitCode  int                       `json:"exit_code"`
	ElapsedMs int                       `json:"elapsed_ms"`
	Error     string                    `json:"error,omitempty"`
	Artifacts []pyodideRunnerArtifactIn `json:"artifacts,omitempty"`
}

type pyodideRunnerArtifactIn struct {
	Name          string `json:"name"`
	MimeType      string `json:"mime_type"`
	SizeBytes     int64  `json:"size_bytes"`
	ContentBase64 string `json:"content_base64"`
}

// NewPyodideRunner creates a runtime adapter for the bundled Pyodide runner.
func NewPyodideRunner(cfg PyodideRunnerConfig) (*PyodideRunner, error) {
	runtimeDir := strings.TrimSpace(cfg.RuntimeDir)
	if runtimeDir == "" {
		runtimeDir = defaultPyodideRuntimeDir
	}
	runnerPath := strings.TrimSpace(cfg.RunnerPath)
	if runnerPath == "" {
		runnerPath = defaultPyodideRunnerPath(runtimeDir)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultPyodideRunnerTimeout
	}
	maxProcessOutput := cfg.MaxProcessOutputBytes
	if maxProcessOutput == 0 {
		maxProcessOutput = defaultPyodideRunnerOutputBytes
	}
	maxResultOutput := cfg.MaxResultOutputBytes
	if maxResultOutput == 0 {
		maxResultOutput = defaultPyodideResultOutputBytes
	}
	if timeout < 0 {
		return nil, errors.New("sandbox: Pyodide runner timeout must not be negative")
	}
	if maxProcessOutput < 0 || maxResultOutput < 0 {
		return nil, errors.New("sandbox: Pyodide runner output limits must not be negative")
	}
	return &PyodideRunner{
		runtimeDir:            runtimeDir,
		runnerPath:            runnerPath,
		runnerArgs:            append([]string(nil), cfg.RunnerArgs...),
		timeout:               timeout,
		environment:           append([]string(nil), cfg.Environment...),
		maxProcessOutputBytes: maxProcessOutput,
		maxResultOutputBytes:  maxResultOutput,
	}, nil
}

func (r *PyodideRunner) Kind() RuntimeKind {
	return RuntimeKindPyodide
}

func (r *PyodideRunner) CheckAvailability() Availability {
	if r == nil {
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    "Pyodide runner not configured",
		}
	}
	probe := ProbePyodideBundle(r.runtimeDir)
	if !probe.Valid {
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    probe.Detail,
		}
	}
	if info, err := os.Stat(r.runnerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Availability{
				Available: false,
				Kind:      RuntimeKindPyodide,
				Detail:    fmt.Sprintf("Pyodide runner missing at %s", r.runnerPath),
			}
		}
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    fmt.Sprintf("checking Pyodide runner: %v", err),
		}
	} else if info.IsDir() {
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    fmt.Sprintf("Pyodide runner path is a directory: %s", r.runnerPath),
		}
	}
	return Availability{
		Available: true,
		Kind:      RuntimeKindPyodide,
		Detail:    "Pyodide runner available",
	}
}

func (r *PyodideRunner) ValidateCode(_ string) error {
	return nil
}

func (r *PyodideRunner) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	return r.execute(ctx, code, allowNetwork, nil)
}

func (r *PyodideRunner) ExtractXLSX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if r == nil {
		return source.ExtractResult{}, errors.New("sandbox: Pyodide runner not configured")
	}
	tmpDir, err := os.MkdirTemp("", "aura-xlsx-*")
	if err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: create xlsx input dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	inputPath := filepath.Join(tmpDir, "workbook.xlsx")
	if err := os.WriteFile(inputPath, body, 0o600); err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: write xlsx input: %w", err)
	}
	res, err := r.execute(ctx, trustedXLSXExtractorCode, false, []string{inputPath})
	if err != nil {
		return source.ExtractResult{}, err
	}
	if !res.OK {
		return source.ExtractResult{}, fmt.Errorf("source: pyodide extraction failed: %s", res.Stderr)
	}
	md, ok := artifactBytes(res.Artifacts, "extract.md")
	if !ok || len(md) == 0 {
		return source.ExtractResult{}, errors.New("source: pyodide extraction missing extract.md")
	}
	metaBytes, ok := artifactBytes(res.Artifacts, "extract.json")
	if !ok || len(metaBytes) == 0 {
		return source.ExtractResult{}, errors.New("source: pyodide extraction missing extract.json")
	}
	var meta source.ExtractionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return source.ExtractResult{}, fmt.Errorf("source: parse pyodide extract metadata: %w", err)
	}
	return source.ExtractResult{Markdown: string(md), Metadata: meta}, nil
}

func (r *PyodideRunner) ExtractDOCX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if r == nil {
		return source.ExtractResult{}, errors.New("sandbox: Pyodide runner not configured")
	}
	tmpDir, err := os.MkdirTemp("", "aura-docx-*")
	if err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: create docx input dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	inputPath := filepath.Join(tmpDir, "document.docx")
	if err := os.WriteFile(inputPath, body, 0o600); err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: write docx input: %w", err)
	}
	res, err := r.execute(ctx, trustedDOCXExtractorCode, false, []string{inputPath})
	if err != nil {
		return source.ExtractResult{}, err
	}
	if !res.OK {
		return source.ExtractResult{}, fmt.Errorf("source: pyodide extraction failed: %s", res.Stderr)
	}
	md, ok := artifactBytes(res.Artifacts, "extract.md")
	if !ok || len(md) == 0 {
		return source.ExtractResult{}, errors.New("source: pyodide extraction missing extract.md")
	}
	metaBytes, ok := artifactBytes(res.Artifacts, "extract.json")
	if !ok || len(metaBytes) == 0 {
		return source.ExtractResult{}, errors.New("source: pyodide extraction missing extract.json")
	}
	var meta source.ExtractionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return source.ExtractResult{}, fmt.Errorf("source: parse pyodide extract metadata: %w", err)
	}
	return source.ExtractResult{Markdown: string(md), Metadata: meta}, nil
}

func artifactBytes(artifacts []Artifact, name string) ([]byte, bool) {
	for _, artifact := range artifacts {
		if artifact.Name == name {
			return artifact.Bytes, true
		}
	}
	return nil, false
}

func (r *PyodideRunner) execute(ctx context.Context, code string, allowNetwork bool, inputFiles []string) (*Result, error) {
	if r == nil {
		return nil, errors.New("sandbox: Pyodide runner not configured")
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = defaultPyodideRunnerTimeout
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	request := pyodideRunnerRequest{
		Code:                code,
		TimeoutMS:           int(timeout.Milliseconds()),
		AllowNetwork:        allowNetwork,
		Packages:            append([]string(nil), RequiredPyodideImports...),
		InputFiles:          append([]string(nil), inputFiles...),
		OutputFileAllowlist: []string{defaultPyodideOutputDir},
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("sandbox: encoding Pyodide runner request: %w", err)
	}

	args := append([]string(nil), r.runnerArgs...)
	args = append(args, "--runtime-dir", r.runtimeDir)
	cmd := exec.CommandContext(runCtx, r.runnerPath, args...)
	cmd.Stdin = bytes.NewReader(requestJSON)
	cmd.Env = sanitizedPyodideRunnerEnv(r.environment)

	var stdout, stderr limitedBuffer
	stdout.limit = r.maxProcessOutputBytes
	stderr.limit = r.maxProcessOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("sandbox: Pyodide runner timed out after %v", timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("sandbox: Pyodide runner failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var response pyodideRunnerResponse
	if err := json.Unmarshal([]byte(stdout.String()), &response); err != nil {
		return nil, fmt.Errorf("sandbox: parsing runner response: %w", err)
	}
	if response.ElapsedMs == 0 {
		response.ElapsedMs = int(elapsed.Milliseconds())
	}
	if response.Error != "" && response.Stderr == "" {
		response.Stderr = response.Error
	}
	artifacts, err := decodePyodideArtifacts(response.Artifacts)
	if err != nil {
		return nil, err
	}
	return &Result{
		OK:        response.OK,
		Stdout:    clipPyodideOutput(response.Stdout, r.maxResultOutputBytes),
		Stderr:    clipPyodideOutput(response.Stderr, r.maxResultOutputBytes),
		ExitCode:  response.ExitCode,
		ElapsedMs: response.ElapsedMs,
		Artifacts: artifacts,
	}, nil
}

func decodePyodideArtifacts(raw []pyodideRunnerArtifactIn) ([]Artifact, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxPyodideArtifacts {
		return nil, fmt.Errorf("sandbox: runner returned %d artifacts, max %d", len(raw), maxPyodideArtifacts)
	}
	artifacts := make([]Artifact, 0, len(raw))
	for i, item := range raw {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("sandbox: artifact[%d] missing name", i)
		}
		if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("sandbox: artifact[%d] name must be a plain filename", i)
		}
		body, err := base64.StdEncoding.DecodeString(item.ContentBase64)
		if err != nil {
			return nil, fmt.Errorf("sandbox: artifact[%d] base64: %w", i, err)
		}
		if len(body) > maxPyodideArtifactBytes {
			return nil, fmt.Errorf("sandbox: artifact[%d] exceeds %d bytes", i, maxPyodideArtifactBytes)
		}
		mimeType := strings.TrimSpace(item.MimeType)
		if mimeType == "" {
			mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		size := item.SizeBytes
		if size == 0 {
			size = int64(len(body))
		}
		if size != int64(len(body)) {
			return nil, fmt.Errorf("sandbox: artifact[%d] size mismatch", i)
		}
		artifacts = append(artifacts, Artifact{
			Name:      name,
			MimeType:  mimeType,
			Bytes:     body,
			SizeBytes: size,
		})
	}
	return artifacts, nil
}

func defaultPyodideRunnerPath(runtimeDir string) string {
	name := "aura-pyodide-runner"
	if runtime.GOOS == "windows" {
		cmdPath := filepath.Join(runtimeDir, "runner", name+".cmd")
		if info, err := os.Stat(cmdPath); err == nil && !info.IsDir() {
			return cmdPath
		}
		name += ".exe"
	}
	return filepath.Join(runtimeDir, "runner", name)
}

func sanitizedPyodideRunnerEnv(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	keep := map[string]bool{
		"APPDATA":      true,
		"HOME":         true,
		"LANG":         true,
		"LC_ALL":       true,
		"LOCALAPPDATA": true,
		"PATH":         true,
		"PATHEXT":      true,
		"SYSTEMROOT":   true,
		"TEMP":         true,
		"TMP":          true,
		"TMPDIR":       true,
		"USERPROFILE":  true,
		"WINDIR":       true,
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToUpper(kv[:eq])
		if keep[key] {
			out = append(out, kv)
		}
	}
	return out
}

func clipPyodideOutput(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...[truncated]"
}

type limitedBuffer struct {
	data      bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 {
		remaining := b.limit - int64(b.data.Len())
		if remaining <= 0 {
			b.truncated = true
			return len(p), nil
		}
		if int64(len(p)) > remaining {
			_, _ = b.data.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
	}
	_, _ = b.data.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b.truncated {
		return b.data.String() + "\n...[truncated]"
	}
	return b.data.String()
}
