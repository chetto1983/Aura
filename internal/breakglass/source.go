package breakglass

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Environment variables that non-interactively source the new operator password
// and the recovery security question/answer for the re-seed (D-03). Each mirrors
// its interactive prompt so `--generate` and fully-non-interactive runs both work
// end-to-end.
const (
	EnvRecoveryPassword = "AURA_RECOVERY_PASSWORD"
	EnvRecoveryQuestion = "AURA_RECOVERY_QUESTION"
	EnvRecoveryAnswer   = "AURA_RECOVERY_ANSWER"
)

// defaultRecoveryQuestion pairs with a `--generate`d (or otherwise un-questioned)
// random answer so the re-seed always has a non-empty question the operator can
// reconfigure later.
const defaultRecoveryQuestion = "Aura break-glass recovery answer"

// generatedSecretBytes yields a 32-char base64url secret, comfortably above the
// 20-char floor R3 requires.
const generatedSecretBytes = 24

// Secrets is the resolved output of Source: the new operator password plus the
// optional recovery question/answer. Generated marks the `--generate` path, whose
// values were emitted once to Stdout.
type Secrets struct {
	Password  string
	Question  string
	Answer    string
	Generated bool
}

// Sourcer resolves the password and recovery Q&A across env vars, `--generate`,
// and a hidden interactive prompt. Every side effect is an injectable seam so the
// decision tree is pure and unit-testable with no real TTY:
//
//   - Getenv reads the AURA_RECOVERY_* env vars,
//   - IsTTY reports whether stdin can be prompted,
//   - ReadHidden performs a single hidden read (the Wave-3 glue backs it with
//     golang.org/x/term; this package stays x/term-free),
//   - Stdout receives the ONE generated-secret line,
//   - Stderr is where the (injected) prompt writes — Source itself never writes a
//     secret to it.
type Sourcer struct {
	Getenv     func(string) string
	IsTTY      func() bool
	ReadHidden func(prompt string) (string, error)
	Stdout     io.Writer
	Stderr     io.Writer
}

// Source resolves the password first, then (unless noRecovery) the recovery Q&A,
// rejecting the env+generate conflict, the non-TTY-with-no-source case, and any
// empty/whitespace value BEFORE returning any Secrets. A generated secret is
// written exactly once to Stdout; no secret ever reaches Stderr or an error.
func (s Sourcer) Source(generate, noRecovery bool) (Secrets, error) {
	password, generated, err := s.sourcePassword(generate)
	if err != nil {
		return Secrets{}, err
	}
	result := Secrets{Password: password, Generated: generated}

	if !noRecovery {
		question, answer, err := s.sourceRecovery(generate)
		if err != nil {
			return Secrets{}, err
		}
		result.Question = question
		result.Answer = answer
	}

	if generated {
		if err := s.emitGenerated(result, noRecovery); err != nil {
			return Secrets{}, err
		}
	}
	return result, nil
}

// sourcePassword applies the env / generate / hidden-prompt decision tree for the
// password, returning whether it was generated.
func (s Sourcer) sourcePassword(generate bool) (string, bool, error) {
	raw := s.getenv(EnvRecoveryPassword)
	switch {
	case generate && raw != "":
		return "", false, errors.New(EnvRecoveryPassword + " is set together with --generate; pass only one password source")
	case generate:
		pw, err := generateSecret(generatedSecretBytes)
		if err != nil {
			return "", false, err
		}
		return pw, true, nil
	case raw != "":
		if strings.TrimSpace(raw) == "" {
			return "", false, errors.New(EnvRecoveryPassword + " is empty")
		}
		return raw, false, nil
	default:
		if !s.canPrompt() {
			return "", false, errors.New("cannot read a password: stdin is not a terminal (set " + EnvRecoveryPassword + " or pass --generate)")
		}
		pw, err := s.promptHiddenConfirmed("New operator password: ", "Confirm operator password: ", "password")
		if err != nil {
			return "", false, err
		}
		return pw, false, nil
	}
}

// sourceRecovery applies the symmetric env / generate / hidden-prompt tree for the
// recovery question + answer. The question defaults when unset; the answer is the
// secret and is rejected when empty/whitespace.
func (s Sourcer) sourceRecovery(generate bool) (string, string, error) {
	if generate {
		answer, err := generateSecret(generatedSecretBytes)
		if err != nil {
			return "", "", err
		}
		return defaultRecoveryQuestion, answer, nil
	}

	question := defaultRecoveryQuestion
	if q := s.getenv(EnvRecoveryQuestion); strings.TrimSpace(q) != "" {
		question = q
	}

	raw := s.getenv(EnvRecoveryAnswer)
	if raw != "" {
		if strings.TrimSpace(raw) == "" {
			return "", "", errors.New(EnvRecoveryAnswer + " is empty")
		}
		return question, raw, nil
	}

	if !s.canPrompt() {
		return "", "", errors.New("cannot read a recovery answer: stdin is not a terminal (set " + EnvRecoveryAnswer + ", pass --generate, or use --no-recovery)")
	}
	answer, err := s.promptHiddenConfirmed("Recovery answer: ", "Confirm recovery answer: ", "recovery answer")
	if err != nil {
		return "", "", err
	}
	return question, answer, nil
}

// promptHiddenConfirmed reads a secret twice via the injected ReadHidden and
// requires the two entries to match and be non-empty. label names the field for
// error messages and is NEVER the secret value itself.
func (s Sourcer) promptHiddenConfirmed(prompt, confirmPrompt, label string) (string, error) {
	first, err := s.ReadHidden(prompt)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	second, err := s.ReadHidden(confirmPrompt)
	if err != nil {
		return "", fmt.Errorf("read %s confirmation: %w", label, err)
	}
	if first != second {
		return "", fmt.Errorf("%s entries do not match", label)
	}
	if strings.TrimSpace(first) == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	return first, nil
}

// emitGenerated writes the single secret-bearing line to Stdout — the ONLY place a
// secret is ever emitted.
func (s Sourcer) emitGenerated(secrets Secrets, noRecovery bool) error {
	if s.Stdout == nil {
		return errors.New("cannot emit the generated secret: no stdout writer configured")
	}
	if noRecovery {
		_, err := fmt.Fprintf(s.Stdout, "generated operator password: %s\n", secrets.Password)
		return err
	}
	_, err := fmt.Fprintf(s.Stdout, "generated operator password: %s | recovery answer: %s (question: %s)\n",
		secrets.Password, secrets.Answer, secrets.Question)
	return err
}

// canPrompt reports whether both prompt seams are wired and stdin is a terminal.
func (s Sourcer) canPrompt() bool {
	return s.IsTTY != nil && s.IsTTY() && s.ReadHidden != nil
}

// getenv reads key via the injected seam, tolerating a nil Getenv (zero value).
func (s Sourcer) getenv(key string) string {
	if s.Getenv == nil {
		return ""
	}
	return s.Getenv(key)
}

// generateSecret returns a URL-safe random string from nBytes of crypto/rand
// entropy (base64.RawURLEncoding, charset [A-Za-z0-9_-]). Mirrors the online
// newPasswordResetToken generator.
func generateSecret(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
