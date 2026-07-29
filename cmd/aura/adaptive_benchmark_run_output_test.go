package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAdaptiveBenchmarkReportCreatesPrivateNonReplacingArtifact(
	t *testing.T,
) {
	t.Parallel()
	outputDir := filepath.Join(t.TempDir(), "benchmark")
	payload := []byte(`{"schema_id":"test"}`)
	path, err := writeAdaptiveBenchmarkReport(outputDir, payload)
	if err != nil {
		t.Fatalf("writeAdaptiveBenchmarkReport: %v", err)
	}
	if path != filepath.Join(outputDir, adaptiveBenchmarkReportFilename) {
		t.Fatalf("path=%q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(payload)+"\n" {
		t.Fatalf("artifact=%q error=%v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("artifact permissions=%#o", info.Mode().Perm())
	}
	if _, err := writeAdaptiveBenchmarkReport(
		outputDir,
		[]byte(`{"schema_id":"replacement"}`),
	); !errors.Is(err, os.ErrExist) {
		t.Fatalf("replacement error=%v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(payload)+"\n" {
		t.Fatalf("artifact replaced=%q error=%v", unchanged, err)
	}
}

func TestWriteAdaptiveBenchmarkReportRejectsUnsafeOutputAndEmptyPayload(
	t *testing.T,
) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name    string
		path    string
		payload []byte
	}{
		{name: "empty", path: filepath.Join(root, "empty")},
		{name: "relative", path: "relative", payload: []byte("{}")},
		{
			name: "noncanonical",
			path: filepath.Join(root, "x") +
				string(os.PathSeparator) + ".." +
				string(os.PathSeparator) + "y",
			payload: []byte("{}"),
		},
		{name: "file", path: file, payload: []byte("{}")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if path, err := writeAdaptiveBenchmarkReport(
				testCase.path,
				testCase.payload,
			); err == nil {
				t.Fatalf("accepted output path %q", path)
			}
		})
	}
}
