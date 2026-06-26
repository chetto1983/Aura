package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunDirSweepIntervalDefaultAndOverride asserts the M-06 periodic-sweep cadence
// knob: a 1h default when unset, and an env override (incl. the <=0 disable value).
func TestRunDirSweepIntervalDefaultAndOverride(t *testing.T) {
	clearPostgresEnv(t)
	if cfg := LoadDB(); cfg.RunDirSweepIntervalSec != 3600 {
		t.Errorf("RunDirSweepIntervalSec: want 3600 default, got %d", cfg.RunDirSweepIntervalSec)
	}
	t.Setenv("AURA_RUN_DIR_SWEEP_INTERVAL_SEC", "0") // <=0 disables the periodic worker
	if cfg := LoadDB(); cfg.RunDirSweepIntervalSec != 0 {
		t.Errorf("RunDirSweepIntervalSec: want 0 (disabled) override, got %d", cfg.RunDirSweepIntervalSec)
	}
}

// TestRunDirAbsoluteNormalization covers F-041: a relative AURA_RUN_DIR is
// normalized to an absolute path at load (sidecars must not be cwd-dependent),
// an already-absolute value is preserved (filepath.Abs is idempotent), and the
// unset case yields the absolute per-user default. RunDirErr stays nil throughout.
func TestRunDirAbsoluteNormalization(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	absInput := filepath.Join(cwd, "runs-abs")

	tests := []struct {
		name string
		set  bool
		env  string
		want string // "" → assert against the absolute default heuristics
	}{
		{name: "relative is normalized to absolute", set: true, env: "./runs", want: filepath.Join(cwd, "runs")},
		{name: "bare relative segment is normalized", set: true, env: "runs", want: filepath.Join(cwd, "runs")},
		{name: "absolute is preserved", set: true, env: absInput, want: absInput},
		{name: "unset uses absolute default", set: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPostgresEnv(t)
			if tt.set {
				t.Setenv("AURA_RUN_DIR", tt.env)
			}
			cfg := LoadDB()
			if cfg.RunDirErr != nil {
				t.Fatalf("RunDirErr: want nil, got %v", cfg.RunDirErr)
			}
			if !filepath.IsAbs(cfg.RunDir) {
				t.Errorf("RunDir: want absolute, got %q", cfg.RunDir)
			}
			if tt.want != "" {
				wantAbs, absErr := filepath.Abs(tt.env)
				if absErr != nil {
					t.Fatalf("filepath.Abs(%q): %v", tt.env, absErr)
				}
				if cfg.RunDir != wantAbs {
					t.Errorf("RunDir: want %q (filepath.Abs of input), got %q", wantAbs, cfg.RunDir)
				}
				if cfg.RunDir != tt.want {
					t.Errorf("RunDir: want %q, got %q", tt.want, cfg.RunDir)
				}
			} else if cfg.RunDir != defaultRunDir() {
				t.Errorf("RunDir: want absolute default %q, got %q", defaultRunDir(), cfg.RunDir)
			}
		})
	}
}
