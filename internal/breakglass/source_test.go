package breakglass

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// Fixed sentinels so the no-leak assertions can prove an operator-supplied secret
// never reaches stderr nor a returned error string.
const (
	sentinelPassword = "S3nt1nel-P4ssw0rd-do-not-leak"
	sentinelAnswer   = "S3nt1nel-4nsw3r-do-not-leak"
)

var generatedSecretRe = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

// seqReader returns a ReadHidden stub yielding the given values in order and
// failing once exhausted, so an unexpected extra prompt is a hard error not a hang.
func seqReader(values ...string) func(string) (string, error) {
	i := 0
	return func(string) (string, error) {
		if i >= len(values) {
			return "", errors.New("unexpected extra hidden prompt")
		}
		v := values[i]
		i++
		return v, nil
	}
}

func assertSingleLine(t *testing.T, stdout string) {
	t.Helper()
	if stdout == "" {
		t.Fatal("expected a generated-secret line on stdout, got none")
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout line is not newline-terminated: %q", stdout)
	}
	if strings.Contains(strings.TrimSuffix(stdout, "\n"), "\n") {
		t.Fatalf("expected exactly one stdout line, got %q", stdout)
	}
}

func TestSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		env        map[string]string
		tty        bool
		readHidden func(string) (string, error)
		nilStdout  bool
		generate   bool
		noRecovery bool
		wantErr    bool
		check      func(t *testing.T, got Secrets, stdout, stderr string)
	}{
		{
			name:       "env password only",
			env:        map[string]string{EnvRecoveryPassword: sentinelPassword},
			noRecovery: true,
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if got.Password != sentinelPassword {
					t.Fatalf("Password = %q, want the env value", got.Password)
				}
				if got.Generated {
					t.Fatal("Generated = true, want false for the env path")
				}
				if stdout != "" || stderr != "" {
					t.Fatalf("env path wrote output: stdout=%q stderr=%q", stdout, stderr)
				}
			},
		},
		{
			name: "env password with question and answer",
			env: map[string]string{
				EnvRecoveryPassword: sentinelPassword,
				EnvRecoveryQuestion: "favourite colour",
				EnvRecoveryAnswer:   sentinelAnswer,
			},
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if got.Password != sentinelPassword {
					t.Fatalf("Password = %q", got.Password)
				}
				if got.Question != "favourite colour" || got.Answer != sentinelAnswer {
					t.Fatalf("Q/A = %q/%q, want the env values", got.Question, got.Answer)
				}
			},
		},
		{
			name: "env answer falls back to the default question",
			env: map[string]string{
				EnvRecoveryPassword: sentinelPassword,
				EnvRecoveryAnswer:   sentinelAnswer,
			},
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if got.Question != defaultRecoveryQuestion {
					t.Fatalf("Question = %q, want the default %q", got.Question, defaultRecoveryQuestion)
				}
				if got.Answer != sentinelAnswer {
					t.Fatalf("Answer = %q", got.Answer)
				}
			},
		},
		{
			name:     "generate password and answer",
			env:      map[string]string{},
			generate: true,
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if !got.Generated {
					t.Fatal("Generated = false, want true")
				}
				if !generatedSecretRe.MatchString(got.Password) {
					t.Fatalf("generated Password %q does not match %s", got.Password, generatedSecretRe)
				}
				if !generatedSecretRe.MatchString(got.Answer) {
					t.Fatalf("generated Answer %q does not match %s", got.Answer, generatedSecretRe)
				}
				if got.Question != defaultRecoveryQuestion {
					t.Fatalf("Question = %q, want the default", got.Question)
				}
				assertSingleLine(t, stdout)
				if !strings.Contains(stdout, got.Password) || !strings.Contains(stdout, got.Answer) {
					t.Fatal("stdout line does not carry both generated secrets")
				}
				if strings.Contains(stderr, got.Password) || strings.Contains(stderr, got.Answer) {
					t.Fatal("a generated secret leaked into stderr")
				}
			},
		},
		{
			name:       "generate with no-recovery emits only the password",
			env:        map[string]string{},
			generate:   true,
			noRecovery: true,
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if !got.Generated || !generatedSecretRe.MatchString(got.Password) {
					t.Fatalf("want a generated password, got %+v", got)
				}
				if got.Answer != "" || got.Question != "" {
					t.Fatalf("no-recovery should not source Q/A, got %q/%q", got.Question, got.Answer)
				}
				assertSingleLine(t, stdout)
				if !strings.Contains(stdout, got.Password) {
					t.Fatal("stdout does not carry the generated password")
				}
			},
		},
		{
			name:     "env password and generate conflict",
			env:      map[string]string{EnvRecoveryPassword: sentinelPassword},
			generate: true,
			wantErr:  true,
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if stdout != "" {
					t.Fatalf("conflict wrote to stdout: %q", stdout)
				}
			},
		},
		{
			name:       "non-tty with no source cannot prompt",
			env:        map[string]string{},
			tty:        false,
			noRecovery: true,
			wantErr:    true,
		},
		{
			name:       "empty whitespace env password",
			env:        map[string]string{EnvRecoveryPassword: "   "},
			noRecovery: true,
			wantErr:    true,
		},
		{
			name: "empty whitespace env answer",
			env: map[string]string{
				EnvRecoveryPassword: sentinelPassword,
				EnvRecoveryAnswer:   "   ",
			},
			wantErr: true,
		},
		{
			name:    "non-tty with no answer source cannot prompt",
			env:     map[string]string{EnvRecoveryPassword: sentinelPassword},
			tty:     false,
			wantErr: true,
		},
		{
			name:       "tty hidden prompt for password and answer",
			env:        map[string]string{},
			tty:        true,
			readHidden: seqReader("prompted-secret", "prompted-secret", "prompted-answer", "prompted-answer"),
			check: func(t *testing.T, got Secrets, stdout, stderr string) {
				if got.Password != "prompted-secret" {
					t.Fatalf("Password = %q, want the prompted value", got.Password)
				}
				if got.Answer != "prompted-answer" {
					t.Fatalf("Answer = %q, want the prompted value", got.Answer)
				}
				if got.Question != defaultRecoveryQuestion {
					t.Fatalf("Question = %q, want the default", got.Question)
				}
				if got.Generated {
					t.Fatal("Generated = true on the prompt path")
				}
				if stdout != "" {
					t.Fatalf("prompt path wrote to stdout: %q", stdout)
				}
			},
		},
		{
			name:       "tty password confirmation mismatch",
			env:        map[string]string{},
			tty:        true,
			readHidden: seqReader("first", "second"),
			wantErr:    true,
		},
		{
			name:       "tty answer confirmation mismatch",
			env:        map[string]string{EnvRecoveryPassword: sentinelPassword},
			tty:        true,
			readHidden: seqReader("first-answer", "second-answer"),
			wantErr:    true,
		},
		{
			name:       "tty empty password at the prompt is rejected",
			env:        map[string]string{},
			tty:        true,
			readHidden: seqReader("   ", "   "), // both entries match but are whitespace
			wantErr:    true,
		},
		{
			name:       "hidden read fails on the first prompt",
			env:        map[string]string{},
			tty:        true,
			readHidden: seqReader(), // errors immediately
			wantErr:    true,
		},
		{
			name:       "hidden read fails on the confirmation prompt",
			env:        map[string]string{},
			tty:        true,
			readHidden: seqReader("only-one"), // second read errors
			wantErr:    true,
		},
		{
			name:      "generate with no stdout errors",
			env:       map[string]string{},
			generate:  true,
			nilStdout: true,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			s := Sourcer{
				Getenv:     func(k string) string { return tt.env[k] },
				IsTTY:      func() bool { return tt.tty },
				ReadHidden: tt.readHidden,
				Stdout:     &stdout,
				Stderr:     &stderr,
			}
			if tt.nilStdout {
				s.Stdout = nil
			}

			got, err := s.Source(tt.generate, tt.noRecovery)

			// No operator-supplied secret may ever reach stderr or an error string.
			for _, secret := range []string{sentinelPassword, sentinelAnswer} {
				if strings.Contains(stderr.String(), secret) {
					t.Fatalf("a secret leaked into stderr: %q", stderr.String())
				}
				if err != nil && strings.Contains(err.Error(), secret) {
					t.Fatalf("a secret leaked into the error string: %v", err)
				}
			}

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Source(%v,%v) = %+v, nil; want an error", tt.generate, tt.noRecovery, got)
				}
			} else if err != nil {
				t.Fatalf("Source(%v,%v) unexpected error: %v", tt.generate, tt.noRecovery, err)
			}
			if tt.check != nil {
				tt.check(t, got, stdout.String(), stderr.String())
			}
		})
	}
}

// TestSourceNilGetenv proves the zero value is useful: with only Stdout wired
// (Getenv/IsTTY/ReadHidden all nil), the --generate + --no-recovery path resolves
// a password without touching env or a terminal.
func TestSourceNilGetenv(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	s := Sourcer{Stdout: &stdout}

	got, err := s.Source(true /*generate*/, true /*noRecovery*/)
	if err != nil {
		t.Fatalf("Source with a nil Getenv: %v", err)
	}
	if !got.Generated || !generatedSecretRe.MatchString(got.Password) {
		t.Fatalf("want a generated password, got %+v", got)
	}
	assertSingleLine(t, stdout.String())
}

// TestGenerateSecret pins the generator's charset, length floor, and randomness.
func TestGenerateSecret(t *testing.T) {
	t.Parallel()

	a, err := generateSecret(generatedSecretBytes)
	if err != nil {
		t.Fatalf("generateSecret: %v", err)
	}
	if len(a) < 20 || !generatedSecretRe.MatchString(a) {
		t.Fatalf("generateSecret = %q, want >= 20 chars over [A-Za-z0-9_-]", a)
	}
	b, err := generateSecret(generatedSecretBytes)
	if err != nil {
		t.Fatalf("generateSecret: %v", err)
	}
	if a == b {
		t.Fatal("generateSecret returned identical values on two calls; not random")
	}
}
