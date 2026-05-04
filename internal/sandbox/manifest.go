package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const PyodideManifestFilename = "aura-pyodide-manifest.json"

// RequiredPyodideImports is the office/data package profile Aura expects the
// bundled Pyodide runtime to provide offline.
var RequiredPyodideImports = []string{
	"numpy",
	"pandas",
	"scipy",
	"statsmodels",
	"matplotlib",
	"PIL",
	"fitz",
	"bs4",
	"lxml",
	"html5lib",
	"pyarrow",
	"python_calamine",
	"xlrd",
	"requests",
	"yaml",
	"dateutil",
	"pytz",
	"tzdata",
	"regex",
	"rich",
}

var requiredPyodideRuntimeFileGroups = [][]string{
	{"pyodide.js", "pyodide.mjs"},
	{"pyodide.asm.wasm"},
	{"python_stdlib.zip"},
	{"repodata.json"},
}

// PyodideManifest describes the release-bundled Pyodide runtime.
type PyodideManifest struct {
	SchemaVersion  int                      `json:"schema_version"`
	Runtime        string                   `json:"runtime"`
	PyodideVersion string                   `json:"pyodide_version"`
	Files          []PyodideManifestFile    `json:"files"`
	Packages       []PyodideManifestPackage `json:"packages"`
	SmokeImports   []string                 `json:"smoke_imports"`
}

type PyodideManifestFile struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Required bool   `json:"required"`
}

type PyodideManifestPackage struct {
	Name       string `json:"name"`
	ImportName string `json:"import_name"`
	Version    string `json:"version,omitempty"`
	Path       string `json:"path,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Required   bool   `json:"required"`
}

// PyodideBundleProbe is a startup diagnostic for the local Pyodide bundle.
// Valid means the bundle contract is present and hash-checked; it does not
// mean execute_code is enabled. The runner adapter controls execution.
type PyodideBundleProbe struct {
	Valid          bool
	RuntimeDir     string
	ManifestPath   string
	PyodideVersion string
	Detail         string
}

// ProbePyodideBundle validates the local runtime bundle and returns a
// dashboard/log-friendly diagnostic.
func ProbePyodideBundle(runtimeDir string) PyodideBundleProbe {
	runtimeDir = strings.TrimSpace(runtimeDir)
	probe := PyodideBundleProbe{RuntimeDir: runtimeDir}
	if runtimeDir == "" {
		probe.Detail = "SANDBOX_RUNTIME_DIR is empty"
		return probe
	}

	manifest, manifestPath, err := LoadPyodideManifest(runtimeDir)
	probe.ManifestPath = manifestPath
	if err != nil {
		probe.Detail = err.Error()
		return probe
	}
	probe.PyodideVersion = manifest.PyodideVersion
	probe.Valid = true
	probe.Detail = "Pyodide bundle manifest valid; runner adapter not configured, execute_code disabled"
	return probe
}

// LoadPyodideManifest reads and validates aura-pyodide-manifest.json from
// runtimeDir. Paths inside the manifest are always relative to runtimeDir.
func LoadPyodideManifest(runtimeDir string) (*PyodideManifest, string, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeDir))
	if err != nil {
		return nil, "", fmt.Errorf("sandbox: resolving runtime dir: %w", err)
	}
	manifestPath := filepath.Join(root, PyodideManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, manifestPath, fmt.Errorf("sandbox: Pyodide manifest missing at %s", manifestPath)
		}
		return nil, manifestPath, fmt.Errorf("sandbox: reading Pyodide manifest: %w", err)
	}

	var manifest PyodideManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, manifestPath, fmt.Errorf("sandbox: parsing Pyodide manifest: %w", err)
	}
	if err := validatePyodideManifest(root, &manifest); err != nil {
		return nil, manifestPath, err
	}
	return &manifest, manifestPath, nil
}

func validatePyodideManifest(root string, manifest *PyodideManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("sandbox: unsupported Pyodide manifest schema_version %d", manifest.SchemaVersion)
	}
	if manifest.Runtime != string(RuntimeKindPyodide) {
		return fmt.Errorf("sandbox: manifest runtime %q, want %q", manifest.Runtime, RuntimeKindPyodide)
	}
	if strings.TrimSpace(manifest.PyodideVersion) == "" {
		return errors.New("sandbox: Pyodide manifest missing pyodide_version")
	}

	fileEntries := make(map[string]PyodideManifestFile, len(manifest.Files))
	for _, file := range manifest.Files {
		normalized, err := normalizeManifestPath(file.Path)
		if err != nil {
			return fmt.Errorf("sandbox: invalid manifest file path %q: %w", file.Path, err)
		}
		fileEntries[normalized] = file
		if file.Required {
			if err := verifyManifestHash(root, normalized, file.SHA256); err != nil {
				return err
			}
		}
	}
	for _, group := range requiredPyodideRuntimeFileGroups {
		file, ok := firstManifestFileInGroup(fileEntries, group)
		if !ok {
			return fmt.Errorf("sandbox: Pyodide manifest missing required runtime file %s", strings.Join(group, " or "))
		}
		if err := verifyManifestHash(root, file.Path, file.SHA256); err != nil {
			return err
		}
	}

	imports := make(map[string]bool, len(manifest.Packages)+len(manifest.SmokeImports))
	for _, pkg := range manifest.Packages {
		importName := strings.TrimSpace(pkg.ImportName)
		if importName == "" {
			importName = strings.TrimSpace(pkg.Name)
		}
		if importName != "" {
			imports[importName] = true
		}
		if pkg.Path != "" {
			normalized, err := normalizeManifestPath(pkg.Path)
			if err != nil {
				return fmt.Errorf("sandbox: invalid package path %q: %w", pkg.Path, err)
			}
			if pkg.Required {
				if err := verifyManifestHash(root, normalized, pkg.SHA256); err != nil {
					return err
				}
			}
		}
	}
	for _, importName := range manifest.SmokeImports {
		if trimmed := strings.TrimSpace(importName); trimmed != "" {
			imports[trimmed] = true
		}
	}

	missing := missingRequiredImports(imports)
	if len(missing) > 0 {
		return fmt.Errorf("sandbox: Pyodide manifest missing required imports: %s", strings.Join(missing, ", "))
	}
	return nil
}

func normalizeManifestPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(raw) {
		return "", errors.New("path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes runtime dir")
	}
	return filepath.ToSlash(clean), nil
}

func verifyManifestHash(root, relPath, expectedSHA256 string) error {
	expectedSHA256 = strings.TrimSpace(expectedSHA256)
	if expectedSHA256 == "" {
		return fmt.Errorf("sandbox: manifest entry %s missing sha256", relPath)
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil || len(expectedSHA256) != sha256.Size*2 {
		return fmt.Errorf("sandbox: manifest entry %s has invalid sha256", relPath)
	}

	fullPath, err := containedPath(root, relPath)
	if err != nil {
		return err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("sandbox: required Pyodide file missing: %s", relPath)
		}
		return fmt.Errorf("sandbox: opening %s: %w", relPath, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("sandbox: hashing %s: %w", relPath, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expectedSHA256) {
		return fmt.Errorf("sandbox: sha256 mismatch for %s", relPath)
	}
	return nil
}

func containedPath(root, relPath string) (string, error) {
	clean, err := normalizeManifestPath(relPath)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", fmt.Errorf("sandbox: checking path containment: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("sandbox: manifest path escapes runtime dir")
	}
	return fullPath, nil
}

func firstManifestFileInGroup(files map[string]PyodideManifestFile, group []string) (PyodideManifestFile, bool) {
	for _, needle := range group {
		if file, ok := files[needle]; ok {
			file.Path = needle
			return file, true
		}
	}
	return PyodideManifestFile{}, false
}

func missingRequiredImports(imports map[string]bool) []string {
	var missing []string
	for _, importName := range RequiredPyodideImports {
		if !imports[importName] {
			missing = append(missing, importName)
		}
	}
	sort.Strings(missing)
	return missing
}
