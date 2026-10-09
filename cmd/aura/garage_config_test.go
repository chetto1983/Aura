package main

import (
	"regexp"
	"testing"
)

// A single-node Garage has no replica to rebuild what a power loss corrupts, so both shipped
// configs sync metadata and data before acknowledging a write (prd.md §17, 2026-10-09).
func TestGarageConfigsSyncBeforeAcknowledging(t *testing.T) {
	root := repoRootForTest(t)
	for _, rel := range []string{"docker/garage/garage.toml", "docker/garage/garage.ipv4.toml"} {
		config := readProjectFile(t, root, rel)
		for _, key := range []string{"metadata_fsync", "data_fsync"} {
			if !regexp.MustCompile(`(?m)^` + key + ` = true$`).MatchString(config) {
				t.Errorf("%s must set %s = true", rel, key)
			}
		}
	}
}
