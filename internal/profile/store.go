// Package profile stores per-identity Agent.md profiles on disk.
package profile

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

const (
	agentFile       = "Agent.md"
	preferencesFile = "preferences.json"
	metadataFile    = "metadata.json"
	changelogFile   = "changelog.md"
)

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

var (
	// ErrInvalidIdentity is returned when a profile identity is empty, malformed, or escapes the profile root.
	ErrInvalidIdentity = errors.New("invalid profile identity")
	// ErrProfileNotFound is returned when one or more required profile files are missing.
	ErrProfileNotFound = errors.New("profile not found")
)

// Preferences is the structured companion file beside Agent.md.
type Preferences struct {
	Lang                string `json:"lang,omitempty"`
	Timezone            string `json:"timezone,omitempty"`
	Location            string `json:"location,omitempty"`
	VoiceMode           bool   `json:"voice_mode,omitempty"`
	CanProactiveMessage bool   `json:"can_proactive_message,omitempty"`
	TonePreference      string `json:"tone_preference,omitempty"`
	ResponseLength      string `json:"response_length,omitempty"`
}

// Metadata tracks profile versioning and onboarding state.
type Metadata struct {
	Version             int    `json:"version"`
	SchemaVersion       int    `json:"schema_version,omitempty"`
	OnboardingCompleted bool   `json:"onboarding_completed"`
	OnboardingSkipped   bool   `json:"onboarding_skipped,omitempty"`
	GeneratedAt         string `json:"generated_at,omitempty"`
	LastUpdatedAt       string `json:"last_updated_at,omitempty"`
}

// Profile is the write input for one identity profile.
type Profile struct {
	AgentMD     string
	Preferences Preferences
	Metadata    Metadata
	Change      string
}

// LoadedProfile is the full profile state reconstructed from disk.
type LoadedProfile struct {
	AgentMD     string
	Preferences Preferences
	Metadata    Metadata
	Changelog   string
}

// Store reads and writes profile files under Root/<identity>/.
type Store struct {
	root string
	now  func() time.Time
}

// NewStore builds a Store. An empty root falls back to DefaultRoot.
func NewStore(root string) *Store {
	if root == "" {
		root = DefaultRoot()
	}
	return &Store{root: root, now: func() time.Time { return time.Now().UTC() }}
}

// DefaultRoot returns the default per-user Aura profile directory.
func DefaultRoot() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aura", "agents")
	}
	return filepath.Join(os.TempDir(), "aura", "agents")
}

// Root returns the absolute-ish root configured for this store.
func (s *Store) Root() string { return s.root }

// WriteProfile atomically writes every file in a profile directory.
func (s *Store) WriteProfile(identity string, p Profile) error {
	dir, err := s.profileDir(identity)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}
	if err := atomicWrite(filepath.Join(dir, agentFile), []byte(p.AgentMD)); err != nil {
		return fmt.Errorf("write %s: %w", agentFile, err)
	}
	prefs, err := json.MarshalIndent(p.Preferences, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", preferencesFile, err)
	}
	if err := atomicWrite(filepath.Join(dir, preferencesFile), append(prefs, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", preferencesFile, err)
	}
	meta, err := json.MarshalIndent(normalizeMetadata(p.Metadata, s.now()), "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", metadataFile, err)
	}
	if err := atomicWrite(filepath.Join(dir, metadataFile), append(meta, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", metadataFile, err)
	}

	changelogPath := filepath.Join(dir, changelogFile)
	old, err := os.ReadFile(changelogPath) // #nosec G304 -- path is rooted under validated profileDir.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", changelogFile, err)
	}
	next := old
	if strings.TrimSpace(p.Change) != "" {
		entry := fmt.Sprintf("## %s\n\n%s\n\n", s.now().Format(time.RFC3339), strings.TrimSpace(p.Change))
		next = append(next, []byte(entry)...)
	}
	if err := atomicWrite(changelogPath, next); err != nil {
		return fmt.Errorf("write %s: %w", changelogFile, err)
	}
	return nil
}

// ReadProfile reads Agent.md, preferences, metadata, and changelog for identity.
func (s *Store) ReadProfile(identity string) (LoadedProfile, error) {
	dir, err := s.profileDir(identity)
	if err != nil {
		return LoadedProfile{}, err
	}
	agentMD, err := readRequired(filepath.Join(dir, agentFile), agentFile)
	if err != nil {
		return LoadedProfile{}, err
	}
	prefsRaw, err := readRequired(filepath.Join(dir, preferencesFile), preferencesFile)
	if err != nil {
		return LoadedProfile{}, err
	}
	metaRaw, err := readRequired(filepath.Join(dir, metadataFile), metadataFile)
	if err != nil {
		return LoadedProfile{}, err
	}
	changelog, err := readRequired(filepath.Join(dir, changelogFile), changelogFile)
	if err != nil {
		return LoadedProfile{}, err
	}
	var prefs Preferences
	if err := json.Unmarshal(prefsRaw, &prefs); err != nil {
		return LoadedProfile{}, fmt.Errorf("parse %s: %w", preferencesFile, err)
	}
	var meta Metadata
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return LoadedProfile{}, fmt.Errorf("parse %s: %w", metadataFile, err)
	}
	return LoadedProfile{
		AgentMD:     string(agentMD),
		Preferences: prefs,
		Metadata:    meta,
		Changelog:   string(changelog),
	}, nil
}

func readRequired(path, name string) ([]byte, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- callers pass validated profile paths.
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", name, ErrProfileNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return b, nil
}

func normalizeMetadata(m Metadata, now time.Time) Metadata {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	if m.GeneratedAt == "" {
		m.GeneratedAt = now.Format(time.RFC3339)
	}
	if m.LastUpdatedAt == "" {
		m.LastUpdatedAt = now.Format(time.RFC3339)
	}
	return m
}

// RootIdentityDir joins root and identity behind the traversal-safe containment guard
// (identityPattern charset + ".."/slash reject + filepath.Rel "escapes root" assertion)
// and returns the absolute per-identity directory WITHOUT creating it. It is the shared
// rooting primitive the mcp and skills packages reuse for their per-identity roots
// (~/.aura/mcp/{id}, $AURA_SKILLS_DIR/{id}, ~/.aura/pyscripts/{id}), so there is exactly
// one path-traversal guard for per-identity filesystem rooting (D-20/D-21). An empty or
// malformed identity, or one that escapes root, yields ErrInvalidIdentity.
func RootIdentityDir(root, identity string) (string, error) {
	if err := ValidateIdentity(identity); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve identity root: %w", err)
	}
	dir, err := filepath.Abs(filepath.Join(absRoot, identity))
	if err != nil {
		return "", fmt.Errorf("resolve identity dir: %w", err)
	}
	rel, err := filepath.Rel(absRoot, dir)
	if err != nil {
		return "", fmt.Errorf("check identity containment: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q escapes identity root", ErrInvalidIdentity, identity)
	}
	return dir, nil
}

// ValidateIdentity reports whether identity is a safe per-identity namespace key:
// it must match identityPattern (charset + length) and contain no ".." or path
// separator. It is the single traversal/charset guard reused wherever an identity
// is mapped to a namespaced resource — a filesystem root (RootIdentityDir) or an
// object-store bucket name (internal/objectstore/garageadmin) — so injection and
// traversal are rejected in exactly one place. An empty or malformed identity
// yields ErrInvalidIdentity.
func ValidateIdentity(identity string) error {
	if !identityPattern.MatchString(identity) || strings.Contains(identity, "..") ||
		strings.ContainsAny(identity, `/\`) {
		return fmt.Errorf("%w: %q must match %s and contain no traversal", ErrInvalidIdentity, identity, identityPattern.String())
	}
	return nil
}

func (s *Store) profileDir(identity string) (string, error) {
	return RootIdentityDir(s.root, identity)
}

func atomicWrite(path string, data []byte) error {
	return atomicWriteWithReplace(path, data, replaceFile)
}

func atomicWriteWithReplace(path string, data []byte, replace func(string, string) error) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
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
	if err := replace(tmpName, path); err != nil {
		return err
	}
	committed = true
	_ = syncDirBestEffort(dir)
	return nil
}

func syncDirBestEffort(dir string) (err error) {
	f, err := os.Open(dir) // #nosec G304 -- internal caller passes a profile directory.
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	return f.Sync()
}
