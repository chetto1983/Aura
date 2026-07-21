// Package idroot is the single traversal-safe per-identity filesystem-rooting
// guard (D-20/D-21). It maps an untrusted identity key to a namespaced resource
// — a filesystem root or an object-store bucket name — behind one charset/
// traversal check, so path-traversal and bucket-name injection are rejected in
// exactly one place. It imports only the standard library, so every per-identity
// consumer (mcp, skills, objectstore, config, provisioning) can depend on it
// without a cycle.
package idroot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ErrInvalidIdentity is returned when an identity is empty, malformed, or escapes its root.
var ErrInvalidIdentity = errors.New("invalid profile identity")

// DefaultRoot returns the default per-user Aura agents directory.
func DefaultRoot() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aura", "agents")
	}
	return filepath.Join(os.TempDir(), "aura", "agents")
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
	return containedDir(root, identity)
}

// containedDir resolves root/identity to an absolute path and asserts it stays
// strictly under root. It is the defense-in-depth containment half of
// RootIdentityDir, split out so the traversal assertion is unit-tested directly
// with inputs ValidateIdentity would otherwise reject upstream.
func containedDir(root, identity string) (string, error) {
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
