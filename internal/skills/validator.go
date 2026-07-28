package skills

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// skillNameRe is the skill-name grammar: 1..64 of [a-z0-9-]. Compiled once at
// package init, mirroring identity.capNameRe. The dir name a skill lives under
// MUST equal its frontmatter name (D-30), so this same grammar gates both.
var skillNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// maxSkillDescriptionLen bounds the frontmatter description at the write
// boundary (the manifest renders it into the model-visible tool Description; an
// unbounded description is a prefix-cache DoS).
const maxSkillDescriptionLen = 1024

// Sentinel errors so callers (writer 11-04, installer 11-05, CLI) classify
// failures without string-matching. They mirror identity/store.go:39-43.
var (
	// ErrInvalidName is the name-grammar / name!=dir failure.
	ErrInvalidName = errors.New("invalid skill name")
	// ErrBlocklisted is a prompt-injection blocklist hit on the body. The error
	// text carries the matched sequence + byte position so the operator gate can
	// show "exactly which sequences matched and where" (D-27).
	ErrBlocklisted = errors.New("skill body contains a blocklisted injection sequence")
	// ErrInvalidStructure is a description-too-long / body-too-large / bad-type
	// structural rejection.
	ErrInvalidStructure = errors.New("invalid skill structure")
)

// SanitizeName is the SINGLE name chokepoint (D-30). It rejects any name that
// does not match ^[a-z0-9-]{1,64}$ and any name that differs from the directory
// it lives under. Every write/install path funnels through here so the on-disk
// skill identity is canonical and dir-name-matched — no second name validator
// exists anywhere in the package.
func SanitizeName(name, dirName string) error {
	if !skillNameRe.MatchString(name) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidName, name, skillNameRe.String())
	}
	if name != dirName {
		return fmt.Errorf("%w: name %q must equal its directory %q", ErrInvalidName, name, dirName)
	}
	return nil
}

// violatesBlocklist NFKC-normalizes the body FIRST (TR15 compatibility
// composition), then case-folds and literal-substring-matches each blocklist
// entry. The normalize-then-match order is non-negotiable (Pitfall 2): a
// fullwidth/compatibility variant of a blocklisted token collapses to its ASCII
// literal under NFKC and must be caught. It returns the matched blocklist
// sequence, its byte position in the normalized+folded body, and whether a hit
// occurred — the position powers the D-27 operator-gate message.
func violatesBlocklist(body string, blocklist []string) (matched string, pos int, ok bool) {
	folded := strings.ToLower(norm.NFKC.String(body))
	for _, bad := range blocklist {
		if bad == "" {
			continue
		}
		if idx := strings.Index(folded, strings.ToLower(bad)); idx >= 0 {
			return bad, idx, true
		}
	}
	return "", 0, false
}

// ValidateForWrite is the pure write-boundary chokepoint consumed by the writer
// (11-04) and installer (11-05). It runs structure checks (name via
// SanitizeName, description length, body cap, type enum) then the injection
// blocklist — UNLESS allowBlocklisted is set, which is the D-27 operator
// override the CLI passes ONLY after the gate has shown the operator exactly
// which sequence matched. Model-authored create/update always passes false:
// a blocklist hit is a hard reject with no escape (T-11-03-E1).
//
// It is PURE: no DB, no FS, no env reads. The caller supplies the blocklist and
// the body cap from config. It is NOT called from the loader (D-28 — disk is
// operator-trusted; enforcement is write-boundary only).
func ValidateForWrite(fm Frontmatter, body string, blocklist []string, bodyCapBytes int, allowBlocklisted bool) error {
	if err := SanitizeName(fm.Name, fm.Name); err != nil {
		// At the write boundary the dir name is the frontmatter name itself
		// (the writer materializes into a dir named after the name); the grammar
		// check is what matters here. A separate dir-mismatch check belongs to
		// the installer, which knows the on-disk dir.
		return err
	}
	// The loader refuses a skill with no description (loader.go validateStructure), so
	// accepting one here writes an artifact that saves, approves and activates and is
	// then skipped at load with only a WARN — the model is told twice that it worked.
	// The write boundary is the only place that can hand the caller a correctable error.
	if strings.TrimSpace(fm.Description) == "" {
		return fmt.Errorf("%w: description is required — the loader skips a skill that has none", ErrInvalidStructure)
	}
	if len(fm.Description) > maxSkillDescriptionLen {
		return fmt.Errorf("%w: description %d bytes exceeds %d", ErrInvalidStructure, len(fm.Description), maxSkillDescriptionLen)
	}
	if bodyCapBytes > 0 && len(body) > bodyCapBytes {
		return fmt.Errorf("%w: body %d bytes exceeds cap %d", ErrInvalidStructure, len(body), bodyCapBytes)
	}
	if fm.Type != TypeInstruction && fm.Type != TypeSnippet {
		return fmt.Errorf("%w: type %q must be %q or %q", ErrInvalidStructure, fm.Type, TypeInstruction, TypeSnippet)
	}
	if allowBlocklisted {
		return nil
	}
	if matched, pos, ok := violatesBlocklist(body, blocklist); ok {
		return fmt.Errorf("%w: matched %q at byte %d (NFKC-normalized)", ErrBlocklisted, matched, pos)
	}
	return nil
}

// Field names a write-boundary rejection's input, so an authoring UI can anchor the
// message to the control the operator has to fix. It is derived from the SENTINEL, not
// from the message text: the wording is free to change, the classification is not.
//
// ErrInvalidStructure covers description, body cap and type at once, so it returns
// FieldNone — its messages already name what is wrong, and guessing a control to point
// at would sometimes point at the wrong one.
const (
	FieldNone = ""
	FieldName = "name"
	FieldBody = "body"
)

// FieldForWriteError maps a ValidateForWrite rejection onto the input it belongs to.
// A nil error, or one that is not a write-boundary sentinel, is FieldNone.
func FieldForWriteError(err error) string {
	switch {
	case err == nil:
		return FieldNone
	case errors.Is(err, ErrInvalidName):
		return FieldName
	case errors.Is(err, ErrBlocklisted):
		return FieldBody
	default:
		return FieldNone
	}
}

// ValidateNameAgainstDir is the installer's name+dir chokepoint: it asserts both
// the grammar AND that the on-disk directory name equals the frontmatter name.
// The writer (which materializes into a name-derived dir) uses ValidateForWrite;
// the installer (which clones an arbitrary upstream tree) uses this so a cloned
// skill whose dir != its declared name is rejected before materialization.
func ValidateNameAgainstDir(fm Frontmatter, dirName string) error {
	return SanitizeName(fm.Name, dirName)
}
