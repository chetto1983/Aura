package cron

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// systemKinds are what retireLegacyLocalIdentity hands to the operator's identity on enrolment,
// and the two backups a person can schedule under theirs. None of them is that person's to stop.
var systemKinds = []TaskKind{
	KindIdentityPurge, KindRetentionSweep, KindSandboxReap, KindShareExpirySweep, KindSkillTTLSweep,
	KindObservabilityCheck, KindMemoryEmbedBackfill, KindMemoryMentionLink,
	KindBackupPostgres, KindBackupObjects,
}

func TestAnIdentityTaskTheDeactivationStopsIsOneThePersonCouldStop(t *testing.T) {
	t.Parallel()
	for _, kind := range systemKinds {
		if slices.Contains(identityTaskKinds(), string(kind)) || IsCancellableKind(kind) {
			t.Errorf("%s would stop with its owner", kind)
		}
	}
	for _, kind := range []TaskKind{KindReminder, KindAgentJob, KindPIMWatch, KindPIMDigest} {
		if !slices.Contains(identityTaskKinds(), string(kind)) || !IsCancellableKind(kind) {
			t.Errorf("%s would outlive its owner", kind)
		}
	}
}

func TestStoreFakeIdentityTasksBindTheIdentity(t *testing.T) {
	t.Parallel()
	for name, call := range map[string]func(*Store) error{
		"pause":  func(s *Store) error { return s.PauseIdentityTasks(context.Background(), "owner") },
		"delete": func(s *Store) error { return s.DeleteIdentityTasks(context.Background(), "owner") },
	} {
		f := &cronFakeDBTX{}
		if err := call(storeWithFake(f)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(f.gotArgs) != 2 || f.gotArgs[0] != "owner" {
			t.Fatalf("%s bound %+v", name, f.gotArgs)
		}
		if !slices.Equal(f.gotArgs[1].([]string), identityTaskKinds()) {
			t.Fatalf("%s is not scoped to what a person scheduled: %+v", name, f.gotArgs[1])
		}
		if !strings.Contains(f.gotSQL, "identity_id = $1") || !strings.Contains(f.gotSQL, "kind = ANY($2") {
			t.Fatalf("%s is not scoped to the identity's own kinds: %s", name, f.gotSQL)
		}
	}
}

func TestStoreFakeIdentityTasksWrapTheError(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{execErr: errDB})
	if err := s.PauseIdentityTasks(context.Background(), "owner"); !errors.Is(err, errDB) {
		t.Fatalf("pause = %v", err)
	}
	if err := s.DeleteIdentityTasks(context.Background(), "owner"); !errors.Is(err, errDB) {
		t.Fatalf("delete = %v", err)
	}
}
