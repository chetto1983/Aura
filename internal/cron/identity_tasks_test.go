package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
		if len(f.gotArgs) != 1 || f.gotArgs[0] != "owner" {
			t.Fatalf("%s bound %+v", name, f.gotArgs)
		}
		if !strings.Contains(f.gotSQL, "identity_id = $1") {
			t.Fatalf("%s is not scoped to the identity: %s", name, f.gotSQL)
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
