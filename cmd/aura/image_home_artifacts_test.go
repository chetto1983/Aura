package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rawBaseOfHome is where each image home's git mirror serves install.sh from: a fresh install
// that pulls from one home must fetch its files from the same side.
var rawBaseOfHome = map[string]string{
	"ghcr.io/chetto1983":                  "raw.githubusercontent.com/chetto1983/Aura",
	"registry.gitlab.com/chetto1983/aura": "gitlab.com/Chetto1983/aura/-/raw",
}

// imageHomes reads the updater's IMAGE_HOME and IMAGE_HOMES, the one place the image homes are
// declared, and returns the current home and every other one.
func imageHomes(t *testing.T, root string) (string, []string) {
	t.Helper()
	home, others, err := parseImageHomes(readProjectFile(t, root, "deploy/aura-image-update.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return home, others
}

func parseImageHomes(updater string) (string, []string, error) {
	home := regexp.MustCompile(`(?m)^IMAGE_HOME=(\S+)$`).FindStringSubmatch(updater)
	homes := regexp.MustCompile(`(?m)^IMAGE_HOMES=\(([^)]*)\)$`).FindStringSubmatch(updater)
	if home == nil || homes == nil {
		return "", nil, errors.New("deploy/aura-image-update.sh declares no IMAGE_HOME or IMAGE_HOMES")
	}
	var others []string
	declared := false
	for h := range strings.FieldsSeq(homes[1]) {
		if h == home[1] {
			declared = true
			continue
		}
		if rawBaseOfHome[h] == "" {
			return "", nil, fmt.Errorf("image home %s has no raw base in rawBaseOfHome", h)
		}
		others = append(others, h)
	}
	if !declared {
		return "", nil, fmt.Errorf("IMAGE_HOME %s is not one of IMAGE_HOMES", home[1])
	}
	if len(others) == 0 {
		return "", nil, fmt.Errorf("IMAGE_HOMES names only IMAGE_HOME %s: the drift test would look for no other home", home[1])
	}
	return home[1], others, nil
}

// foreignHomeRefs lists every reference text makes to another home: its registry path, or the
// raw-file host its git mirror serves install.sh from.
func foreignHomeRefs(text string, others []string) []string {
	var found []string
	for _, h := range others {
		for _, needle := range []string{h + "/", rawBaseOfHome[h]} {
			if strings.Contains(text, needle) {
				found = append(found, needle)
			}
		}
	}
	return found
}

// With no home but IMAGE_HOME the drift test below would look for no foreign reference at all,
// and pass on any file.
func TestParseImageHomesRefusesASingleHome(t *testing.T) {
	single := "IMAGE_HOME=registry.gitlab.com/chetto1983/aura\nIMAGE_HOMES=(registry.gitlab.com/chetto1983/aura)\n"
	if _, _, err := parseImageHomes(single); err == nil {
		t.Fatal("an IMAGE_HOMES naming only IMAGE_HOME was accepted")
	}
	home, others, err := parseImageHomes("IMAGE_HOME=registry.gitlab.com/chetto1983/aura\nIMAGE_HOMES=(ghcr.io/chetto1983 registry.gitlab.com/chetto1983/aura)\n")
	if err != nil || home != "registry.gitlab.com/chetto1983/aura" || len(others) != 1 || others[0] != "ghcr.io/chetto1983" {
		t.Fatalf("two homes parsed as %q %v %v", home, others, err)
	}
}

func TestForeignHomeRefsFindsTheOtherHome(t *testing.T) {
	others := []string{"ghcr.io/chetto1983"}
	stale := "AURA_IMAGE=ghcr.io/chetto1983/aura:edge\ncurl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/master/scripts/install.sh"
	if got := foreignHomeRefs(stale, others); len(got) != 2 {
		t.Fatalf("a stale file yielded %v, want its registry path and its raw base", got)
	}
	if got := foreignHomeRefs("AURA_IMAGE=registry.gitlab.com/chetto1983/aura/aura:edge", others); len(got) != 0 {
		t.Fatalf("a file on the home yielded %v", got)
	}
}

// Every default a host receives names the home the updater moves hosts to, so a fresh install
// and an updated host pull the same images (docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md).
// The payload manifest is what every host receives; the installer, the env template and the npm
// package reach a host the first time. The updater declares every home by design, so its
// IMAGE_HOMES line is not read.
func TestShippedDefaultsNameTheUpdatersImageHome(t *testing.T) {
	root := repoRootForTest(t)
	home, others := imageHomes(t, root)
	seen := map[string]bool{}
	var shipped []string
	add := func(rel string) {
		if !seen[rel] {
			seen[rel] = true
			shipped = append(shipped, rel)
		}
	}
	for _, rel := range append(payloadPaths(t, root), "scripts/install.sh", ".env.example", "packages/create-aura/README.md") {
		add(rel)
	}
	for _, pattern := range []string{"deploy/*", "packages/create-aura/src/*.ts"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil || len(matches) == 0 {
			t.Fatalf("glob %s: %v %v", pattern, matches, err)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			add(filepath.ToSlash(rel))
		}
	}
	declaration := regexp.MustCompile(`(?m)^IMAGE_HOMES=.*$`)
	for _, rel := range shipped {
		text := readProjectFile(t, root, rel)
		if rel == "deploy/aura-image-update.sh" {
			text = declaration.ReplaceAllString(text, "")
		}
		if refs := foreignHomeRefs(text, others); len(refs) > 0 {
			t.Errorf("%s still names %v; every shipped default names %s", rel, refs, home)
		}
	}
}
