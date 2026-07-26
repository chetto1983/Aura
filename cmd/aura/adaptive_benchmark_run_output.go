package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const adaptiveBenchmarkReportFilename = "adaptive-benchmark-report.json"

func writeAdaptiveBenchmarkReport(
	outputDir string,
	payload []byte,
) (string, error) {
	if outputDir == "" ||
		!filepath.IsAbs(outputDir) ||
		filepath.Clean(outputDir) != outputDir ||
		len(payload) == 0 {
		return "", errors.New(
			"adaptive benchmark report destination is invalid",
		)
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", fmt.Errorf(
			"create adaptive benchmark output directory: %w",
			err,
		)
	}
	info, err := os.Lstat(outputDir)
	if err != nil ||
		!info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New(
			"adaptive benchmark output directory is unsafe",
		)
	}
	finalPath := filepath.Join(
		outputDir,
		adaptiveBenchmarkReportFilename,
	)
	temp, err := os.CreateTemp(
		outputDir,
		".adaptive-benchmark-report-*.tmp",
	)
	if err != nil {
		return "", fmt.Errorf(
			"create adaptive benchmark report temporary file: %w",
			err,
		)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		_ = temp.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return "", fmt.Errorf(
			"protect adaptive benchmark report: %w",
			err,
		)
	}
	if _, err := temp.Write(payload); err != nil {
		return "", fmt.Errorf(
			"write adaptive benchmark report: %w",
			err,
		)
	}
	if _, err := temp.Write([]byte{'\n'}); err != nil {
		return "", fmt.Errorf(
			"terminate adaptive benchmark report: %w",
			err,
		)
	}
	if err := temp.Sync(); err != nil {
		return "", fmt.Errorf(
			"sync adaptive benchmark report: %w",
			err,
		)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf(
			"close adaptive benchmark report: %w",
			err,
		)
	}
	if err := os.Link(tempPath, finalPath); err != nil {
		return "", fmt.Errorf(
			"publish adaptive benchmark report: %w",
			err,
		)
	}
	if err := os.Remove(tempPath); err != nil {
		_ = os.Remove(finalPath)
		return "", fmt.Errorf(
			"remove adaptive benchmark temporary file: %w",
			err,
		)
	}
	removeTemp = false
	// #nosec G304 -- outputDir is canonical, absolute, and Lstat-verified as a directory above.
	if directory, err := os.Open(outputDir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return finalPath, nil
}
