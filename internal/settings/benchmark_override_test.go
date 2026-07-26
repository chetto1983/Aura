package settings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

type errorLister struct{ err error }

func (l errorLister) List(context.Context) ([]sqlc.AuraSettings, error) {
	return nil, l.err
}

func TestQwenPortabilitySettingsAreFrozen(t *testing.T) {
	want := BenchmarkModelSettings{
		Provider: "llamacpp",
		Model:    "qwen",
		BaseURL:  "http://127.0.0.1:18081/v1",
	}
	if QwenPortabilitySettings != want {
		t.Fatalf("QwenPortabilitySettings = %#v, want %#v", QwenPortabilitySettings, want)
	}
}

func TestBeginQwenPortabilityOverrideValidatesBeforeDatabaseAccess(t *testing.T) {
	valid := BenchmarkOverrideRequest{
		RunID:         uuid.Must(uuid.NewV7()),
		OwnerID:       uuid.Must(uuid.NewV7()),
		LeaseDuration: time.Minute,
	}
	tests := []struct {
		name string
		edit func(*BenchmarkOverrideRequest)
	}{
		{name: "missing run", edit: func(r *BenchmarkOverrideRequest) { r.RunID = uuid.Nil }},
		{name: "missing owner", edit: func(r *BenchmarkOverrideRequest) { r.OwnerID = uuid.Nil }},
		{name: "zero lease", edit: func(r *BenchmarkOverrideRequest) { r.LeaseDuration = 0 }},
		{name: "negative lease", edit: func(r *BenchmarkOverrideRequest) { r.LeaseDuration = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			tt.edit(&request)
			_, err := BeginQwenPortabilityOverride(t.Context(), nil, request)
			if !errors.Is(err, ErrInvalidBenchmarkOverride) {
				t.Fatalf("BeginQwenPortabilityOverride error = %v, want ErrInvalidBenchmarkOverride", err)
			}
		})
	}
}

func TestBenchmarkOverrideLeaseIsBounded(t *testing.T) {
	valid := BenchmarkOverrideRequest{
		RunID:         uuid.Must(uuid.NewV7()),
		OwnerID:       uuid.Must(uuid.NewV7()),
		LeaseDuration: 30 * time.Minute,
	}
	if err := validateBenchmarkOverrideRequest(valid); err != nil {
		t.Fatalf("30 minute lease rejected: %v", err)
	}
	valid.LeaseDuration += time.Nanosecond
	if err := validateBenchmarkOverrideRequest(valid); !errors.Is(
		err, ErrInvalidBenchmarkOverride,
	) {
		t.Fatalf("over-limit lease error = %v, want ErrInvalidBenchmarkOverride", err)
	}
}

func TestBenchmarkIntegrationDSNGuard(t *testing.T) {
	tests := []struct {
		name    string
		app     string
		migrate string
		wantErr bool
	}{
		{
			name:    "disposable settings database",
			app:     "postgres://aura_app:pw@localhost/aura_settings_test_123?sslmode=disable",
			migrate: "postgres://aura_migrate:pw@localhost/aura_settings_test_123?sslmode=disable",
		},
		{
			name:    "coverage database",
			app:     "postgres://aura_app:pw@localhost/aura_cov?sslmode=disable",
			migrate: "postgres://aura_migrate:pw@localhost/aura_cov?sslmode=disable",
		},
		{
			name:    "live aura",
			app:     "postgres://aura_app:pw@localhost/aura?sslmode=disable",
			migrate: "postgres://aura_migrate:pw@localhost/aura?sslmode=disable",
			wantErr: true,
		},
		{
			name:    "different databases",
			app:     "postgres://aura_app:pw@localhost/aura_settings_a?sslmode=disable",
			migrate: "postgres://aura_migrate:pw@localhost/aura_settings_b?sslmode=disable",
			wantErr: true,
		},
		{name: "malformed", app: ":", migrate: ":", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBenchmarkIntegrationDSNs(tt.app, tt.migrate)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBenchmarkIntegrationDSNs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRecoverExpiredBenchmarkOverrideRejectsNilPool(t *testing.T) {
	err := RecoverExpiredBenchmarkOverride(t.Context(), nil)
	if !errors.Is(err, ErrInvalidBenchmarkOverride) {
		t.Fatalf("RecoverExpiredBenchmarkOverride error = %v, want ErrInvalidBenchmarkOverride", err)
	}
}

func TestNilBenchmarkOverrideRejectsLifecycleCalls(t *testing.T) {
	var override *BenchmarkOverride
	if err := override.Heartbeat(t.Context()); !errors.Is(err, ErrInvalidBenchmarkOverride) {
		t.Fatalf("nil Heartbeat error = %v, want ErrInvalidBenchmarkOverride", err)
	}
	if err := override.Restore(t.Context()); !errors.Is(err, ErrInvalidBenchmarkOverride) {
		t.Fatalf("nil Restore error = %v, want ErrInvalidBenchmarkOverride", err)
	}
	empty := &BenchmarkOverride{}
	if err := empty.Heartbeat(t.Context()); !errors.Is(err, ErrInvalidBenchmarkOverride) {
		t.Fatalf("empty Heartbeat error = %v, want ErrInvalidBenchmarkOverride", err)
	}
	if err := empty.Restore(t.Context()); !errors.Is(err, ErrInvalidBenchmarkOverride) {
		t.Fatalf("empty Restore error = %v, want ErrInvalidBenchmarkOverride", err)
	}
	releaseBenchmarkSettingsConn(t.Context(), nil)
}

func TestOverlayEnvReturnsListError(t *testing.T) {
	want := errors.New("list failed")
	if err := OverlayEnv(t.Context(), errorLister{err: want}); !errors.Is(err, want) {
		t.Fatalf("OverlayEnv error = %v, want %v", err, want)
	}
}
