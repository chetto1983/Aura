package audio_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/audio"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func TestParse_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want audio.Mode
	}{
		{"off", audio.ModeOff},
		{"voice_only", audio.ModeVoiceOnly},
		{"all", audio.ModeAll},
	}
	for _, tc := range cases {
		got, err := audio.Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q) error = %v, want nil", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParse_Invalid(t *testing.T) {
	for _, bad := range []string{"", "ON", "Voice_Only", "unknown", "1"} {
		_, err := audio.Parse(bad)
		if err == nil {
			t.Errorf("Parse(%q) expected error, got nil", bad)
		}
	}
}

func TestShouldSynth(t *testing.T) {
	cases := []struct {
		m    audio.Mode
		want bool
	}{
		{audio.ModeOff, false},
		{audio.ModeVoiceOnly, true},
		{audio.ModeAll, true},
	}
	for _, tc := range cases {
		if got := audio.ShouldSynth(tc.m); got != tc.want {
			t.Errorf("ShouldSynth(%q) = %v, want %v", tc.m, got, tc.want)
		}
	}
}

func TestShouldSuppressText(t *testing.T) {
	cases := []struct {
		m    audio.Mode
		want bool
	}{
		{audio.ModeOff, false},
		{audio.ModeVoiceOnly, true},
		{audio.ModeAll, false},
	}
	for _, tc := range cases {
		if got := audio.ShouldSuppressText(tc.m); got != tc.want {
			t.Errorf("ShouldSuppressText(%q) = %v, want %v", tc.m, got, tc.want)
		}
	}
}

func TestSQLiteStore_GetDefaultWhenMissing(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	store := audio.NewSQLiteStore(db)
	mode, err := store.Get(context.Background(), "chat-999")
	if err != nil {
		t.Fatalf("Get missing row error = %v, want nil", err)
	}
	if mode != audio.ModeOff {
		t.Errorf("Get missing = %q, want %q", mode, audio.ModeOff)
	}
}

func TestSQLiteStore_SetAndGetRoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	store := audio.NewSQLiteStore(db)
	ctx := context.Background()

	for _, m := range []audio.Mode{audio.ModeAll, audio.ModeVoiceOnly, audio.ModeOff} {
		if err := store.Set(ctx, "chat-1", m); err != nil {
			t.Fatalf("Set(%q) error = %v", m, err)
		}
		got, err := store.Get(ctx, "chat-1")
		if err != nil {
			t.Fatalf("Get after Set(%q) error = %v", m, err)
		}
		if got != m {
			t.Errorf("Get after Set(%q) = %q, want %q", m, got, m)
		}
	}
}

func TestSQLiteStore_IndependentChats(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	store := audio.NewSQLiteStore(db)
	ctx := context.Background()

	if err := store.Set(ctx, "chat-A", audio.ModeAll); err != nil {
		t.Fatalf("Set chat-A: %v", err)
	}
	if err := store.Set(ctx, "chat-B", audio.ModeVoiceOnly); err != nil {
		t.Fatalf("Set chat-B: %v", err)
	}

	a, _ := store.Get(ctx, "chat-A")
	b, _ := store.Get(ctx, "chat-B")
	if a != audio.ModeAll {
		t.Errorf("chat-A = %q, want %q", a, audio.ModeAll)
	}
	if b != audio.ModeVoiceOnly {
		t.Errorf("chat-B = %q, want %q", b, audio.ModeVoiceOnly)
	}
}
