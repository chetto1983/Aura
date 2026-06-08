package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type preferences struct {
	Lang                string `json:"lang"`
	Timezone            string `json:"timezone"`
	VoiceMode           bool   `json:"voice_mode"`
	CanProactiveMessage bool   `json:"can_proactive_message"`
	TonePreference      string `json:"tone_preference"`
	ResponseLength      string `json:"response_length"`
}

type metadata struct {
	Version             int    `json:"version"`
	OnboardingCompleted bool   `json:"onboarding_completed"`
	GeneratedAt         string `json:"generated_at"`
	LastUpdatedAt       string `json:"last_updated_at"`
}

type profile struct {
	AgentMD     string
	Preferences preferences
	Metadata    metadata
	Change      string
}

type loadedProfile struct {
	AgentMD     string
	Preferences preferences
	Metadata    metadata
	Changelog   string
}

type store struct {
	root string
}

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FAIL]", err)
		os.Exit(1)
	}
	fmt.Println("[SUMMARY] VALIDATED phase14 profile store atomic contract")
}

func run() error {
	root := filepath.Join(os.TempDir(), "aura-spike-038-profile-store")
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	s := store{root: root}

	now := time.Now().UTC().Format(time.RFC3339)
	first := profile{
		AgentMD: "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses.\n",
		Preferences: preferences{
			Lang:                "it",
			Timezone:            "Europe/Rome",
			VoiceMode:           true,
			CanProactiveMessage: false,
			TonePreference:      "pragmatic",
			ResponseLength:      "short",
		},
		Metadata: metadata{
			Version:             1,
			OnboardingCompleted: true,
			GeneratedAt:         now,
			LastUpdatedAt:       now,
		},
		Change: "initial onboarding profile",
	}
	if err := s.WriteProfile("local", first); err != nil {
		return fmt.Errorf("initial write: %w", err)
	}
	fmt.Println("[CHECK] initial Agent.md/preferences/metadata/changelog write OK")

	second := first
	second.AgentMD = "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses.\n- Prefer short technical answers.\n"
	second.Metadata.Version = 2
	second.Metadata.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	second.Change = "added response-length preference"
	if err := s.WriteProfile("local", second); err != nil {
		return fmt.Errorf("atomic update: %w", err)
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		return err
	}
	if !strings.Contains(got.AgentMD, "Prefer short technical answers") {
		return fmt.Errorf("updated Agent.md missing new preference")
	}
	if got.Metadata.Version != 2 {
		return fmt.Errorf("metadata version = %d, want 2", got.Metadata.Version)
	}
	if got.Preferences.Lang != "it" || !got.Preferences.VoiceMode {
		return fmt.Errorf("preferences did not round-trip: %+v", got.Preferences)
	}
	if !strings.Contains(got.Changelog, "initial onboarding profile") || !strings.Contains(got.Changelog, second.Change) {
		return fmt.Errorf("changelog did not preserve both entries")
	}
	if err := assertNoTempFiles(filepath.Join(root, "local")); err != nil {
		return err
	}
	fmt.Println("[CHECK] replacement update preserved files and left no temp files")

	for _, id := range []string{"", "../evil", `..\evil`, "a/b", `a\b`, ".hidden", "local..evil"} {
		if _, err := s.profileDir(id); err == nil {
			return fmt.Errorf("unsafe identity %q was accepted", id)
		}
	}
	fmt.Println("[CHECK] identity path traversal rejected")

	fmt.Printf("[INFO] scratch root: %s\n", root)
	return nil
}

func (s store) WriteProfile(identity string, p profile) error {
	dir, err := s.profileDir(identity)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir profile dir: %w", err)
	}
	if err := atomicWrite(filepath.Join(dir, "Agent.md"), []byte(p.AgentMD)); err != nil {
		return fmt.Errorf("write Agent.md: %w", err)
	}
	prefs, err := json.MarshalIndent(p.Preferences, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, "preferences.json"), append(prefs, '\n')); err != nil {
		return fmt.Errorf("write preferences.json: %w", err)
	}
	meta, err := json.MarshalIndent(p.Metadata, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, "metadata.json"), append(meta, '\n')); err != nil {
		return fmt.Errorf("write metadata.json: %w", err)
	}

	entry := fmt.Sprintf("## %s\n\n%s\n\n", time.Now().UTC().Format(time.RFC3339), p.Change)
	changelogPath := filepath.Join(dir, "changelog.md")
	old, err := os.ReadFile(changelogPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read changelog.md: %w", err)
	}
	if err := atomicWrite(changelogPath, append(old, []byte(entry)...)); err != nil {
		return fmt.Errorf("write changelog.md: %w", err)
	}
	return nil
}

func (s store) ReadProfile(identity string) (loadedProfile, error) {
	dir, err := s.profileDir(identity)
	if err != nil {
		return loadedProfile{}, err
	}
	agentMD, err := os.ReadFile(filepath.Join(dir, "Agent.md"))
	if err != nil {
		return loadedProfile{}, err
	}
	prefsRaw, err := os.ReadFile(filepath.Join(dir, "preferences.json"))
	if err != nil {
		return loadedProfile{}, err
	}
	metaRaw, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return loadedProfile{}, err
	}
	changelog, err := os.ReadFile(filepath.Join(dir, "changelog.md"))
	if err != nil {
		return loadedProfile{}, err
	}
	var prefs preferences
	if err := json.Unmarshal(prefsRaw, &prefs); err != nil {
		return loadedProfile{}, err
	}
	var meta metadata
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return loadedProfile{}, err
	}
	return loadedProfile{
		AgentMD:     string(agentMD),
		Preferences: prefs,
		Metadata:    meta,
		Changelog:   string(changelog),
	}, nil
}

func (s store) profileDir(identity string) (string, error) {
	if !identityPattern.MatchString(identity) || strings.Contains(identity, "..") {
		return "", fmt.Errorf("invalid identity %q", identity)
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(filepath.Join(root, identity))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("identity %q escapes profile root", identity)
	}
	return dir, nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	if err := syncDirBestEffort(dir); err != nil {
		fmt.Printf("[INFO] directory sync skipped: %v\n", err)
	}
	return nil
}

func syncDirBestEffort(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func assertNoTempFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			return fmt.Errorf("left temp file behind: %s", entry.Name())
		}
	}
	return nil
}
