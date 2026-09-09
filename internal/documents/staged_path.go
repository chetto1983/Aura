package documents

import (
	"crypto/sha256"
	"fmt"
	pathpkg "path"
	"strings"
)

// StagedDirName is the fixed subdirectory materialized copies land in, under whatever
// root the caller is writing to. The destination is never caller-chosen: a working copy
// of a user document is not something a caller should be able to scatter around.
const StagedDirName = "documents"

// StagedRelativeDirectory derives the single directory one logical document's copies
// live in, relative to the caller's root: "documents/document-<hex>".
//
// Slash-separated and root-agnostic on purpose. The agent writes into its sandbox box
// and `aura docs open` writes onto the host, and they must not derive two layouts that
// can drift apart -- the delete path resolves the parent through this same rule.
func StagedRelativeDirectory(documentID string) (string, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return "", fmt.Errorf("staged document directory: document id is empty")
	}
	sum := sha256.Sum256([]byte(documentID))
	return pathpkg.Join(StagedDirName, fmt.Sprintf("document-%x", sum[:12])), nil
}

// StagedRelativePath validates a bare alias and places it below that directory.
func StagedRelativePath(documentID, fileName string) (string, error) {
	name := strings.TrimSpace(fileName)
	if err := ValidateStagedFileName(name); err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("staged document path: file name is empty")
	}
	dir, err := StagedRelativeDirectory(documentID)
	if err != nil {
		return "", err
	}
	return pathpkg.Join(dir, name), nil
}

// ValidateStagedFileName refuses anything that would stop being a single path component.
// The name becomes one, so a separator would let the join walk out of the document's
// directory and a leading dot would write somewhere nothing looks.
//
// An empty name is allowed here and resolved by the caller from the record's own name;
// StagedRelativePath is where an empty one is finally refused.
func ValidateStagedFileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("file_name %q must be a bare file name, not a path", name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("file_name %q is not a usable file name", name)
	}
	return nil
}
