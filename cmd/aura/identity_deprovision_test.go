package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

// identity_deprovision_test.go covers the four pure seams of `aura identity
// {deactivate|purge}` — argument parsing, name-or-UUID resolution, the protected-target
// guard, and the verb dispatch — with NO pool, NO Docker, NO ArcadeDB and NO Authula,
// mirroring identity_create_test.go's daemon-free seam.

func TestParseIdentityDeprovisionArgs(t *testing.T) {
	t.Run("target plus --confirm is accepted", func(t *testing.T) {
		got, resume, err := parseIdentityDeprovisionArgs([]string{"b@example.invalid", "--confirm"})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != "b@example.invalid" {
			t.Fatalf("target = %q, want b@example.invalid", got)
		}
		if resume {
			t.Fatal("resumeResources = true without --resume-resources")
		}
	})

	t.Run("--confirm may precede the target", func(t *testing.T) {
		got, _, err := parseIdentityDeprovisionArgs([]string{"--confirm", "b@example.invalid"})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != "b@example.invalid" {
			t.Fatalf("target = %q, want b@example.invalid", got)
		}
	})

	t.Run("--resume-resources is recognised", func(t *testing.T) {
		got, resume, err := parseIdentityDeprovisionArgs([]string{
			"6d0ebdee-bf43-4f5b-abf6-469310d52a5a", "--confirm", "--resume-resources",
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != "6d0ebdee-bf43-4f5b-abf6-469310d52a5a" {
			t.Fatalf("target = %q, want the UUID", got)
		}
		if !resume {
			t.Fatal("resumeResources = false, want true")
		}
	})

	t.Run("missing --confirm is refused", func(t *testing.T) {
		_, _, err := parseIdentityDeprovisionArgs([]string{"b@example.invalid"})
		if err == nil {
			t.Fatal("err = nil, want a refusal — confirmation is mandatory")
		}
		if !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("err = %q, want it to name --confirm", err)
		}
	})

	t.Run("missing target is refused", func(t *testing.T) {
		if _, _, err := parseIdentityDeprovisionArgs([]string{"--confirm"}); err == nil {
			t.Fatal("err = nil, want a refusal naming the missing target")
		}
	})

	t.Run("a second target is refused", func(t *testing.T) {
		_, _, err := parseIdentityDeprovisionArgs([]string{"a@example.invalid", "b@example.invalid", "--confirm"})
		if err == nil {
			t.Fatal("err = nil, want a refusal — one target at a time")
		}
	})

	t.Run("unknown flag is refused", func(t *testing.T) {
		_, _, err := parseIdentityDeprovisionArgs([]string{"b@example.invalid", "--force", "--confirm"})
		if err == nil {
			t.Fatal("err = nil, want a refusal naming the unknown flag")
		}
	})
}

// fakeIdentityLookup satisfies identityLookup without a pool. It records which lookup the
// resolver chose so the test can assert a UUID never goes down the by-name path.
type fakeIdentityLookup struct {
	byName map[string]identity.Identity
	byID   map[string]identity.Identity
	caps   map[string][]string

	gotName string
	gotID   string
}

func (f *fakeIdentityLookup) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	f.gotName = name
	id, ok := f.byName[name]
	if !ok {
		return identity.Identity{}, identity.ErrIdentityNotFound
	}
	return id, nil
}

func (f *fakeIdentityLookup) GetIdentityByID(_ context.Context, identityID string) (identity.Identity, error) {
	f.gotID = identityID
	id, ok := f.byID[identityID]
	if !ok {
		return identity.Identity{}, identity.ErrIdentityNotFound
	}
	return id, nil
}

func (f *fakeIdentityLookup) ListCapabilities(_ context.Context, identityID string) ([]string, error) {
	return f.caps[identityID], nil
}

func TestResolveDeprovisionTarget(t *testing.T) {
	const uuidRef = "6d0ebdee-bf43-4f5b-abf6-469310d52a5a"
	want := identity.Identity{ID: uuidRef, Name: "musr-live-run-b@example.invalid", Kind: "user"}

	t.Run("a UUID resolves by id, never by name", func(t *testing.T) {
		lookup := &fakeIdentityLookup{byID: map[string]identity.Identity{uuidRef: want}}
		got, err := resolveDeprovisionTarget(context.Background(), lookup, uuidRef)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got.ID != want.ID {
			t.Fatalf("ID = %q, want %q", got.ID, want.ID)
		}
		if lookup.gotName != "" {
			t.Fatalf("GetIdentityByName was called with %q for a UUID reference", lookup.gotName)
		}
	})

	t.Run("a non-UUID resolves by name", func(t *testing.T) {
		lookup := &fakeIdentityLookup{byName: map[string]identity.Identity{want.Name: want}}
		got, err := resolveDeprovisionTarget(context.Background(), lookup, want.Name)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got.ID != want.ID {
			t.Fatalf("ID = %q, want %q", got.ID, want.ID)
		}
		if lookup.gotID != "" {
			t.Fatalf("GetIdentityByID was called with %q for a name reference", lookup.gotID)
		}
	})

	t.Run("an unknown target reports ErrIdentityNotFound", func(t *testing.T) {
		lookup := &fakeIdentityLookup{}
		_, err := resolveDeprovisionTarget(context.Background(), lookup, "absent@example.invalid")
		if !errors.Is(err, identity.ErrIdentityNotFound) {
			t.Fatalf("err = %v, want ErrIdentityNotFound", err)
		}
	})
}

func TestGuardProtectedIdentity(t *testing.T) {
	t.Run("an ordinary provisioned user is allowed", func(t *testing.T) {
		id := identity.Identity{ID: "6d0ebdee-bf43-4f5b-abf6-469310d52a5a", Name: "b@example.invalid", Kind: "user"}
		if err := guardProtectedIdentity(id, []string{"agent.run"}); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})

	t.Run("the wildcard holder is protected", func(t *testing.T) {
		id := identity.Identity{ID: "bb78065b-0fc2-4c02-b4b7-b9aeceed1511", Name: "operator@example.test", Kind: "user"}
		err := guardProtectedIdentity(id, []string{identity.Wildcard})
		if !errors.Is(err, errProtectedIdentity) {
			t.Fatalf("err = %v, want errProtectedIdentity — '*' is the system-managed operator marker", err)
		}
	})

	t.Run("a non-user identity is protected", func(t *testing.T) {
		id := identity.Identity{ID: "00000000-0000-0000-0000-000000000039", Name: "aura-cli", Kind: "service"}
		if err := guardProtectedIdentity(id, nil); !errors.Is(err, errProtectedIdentity) {
			t.Fatalf("err = %v, want errProtectedIdentity — a service identity is not deprovisionable", err)
		}
	})

	t.Run("the seeded local operator is protected", func(t *testing.T) {
		id := identity.Identity{ID: localSeededIdentityID, Name: "local", Kind: "user"}
		if err := guardProtectedIdentity(id, nil); !errors.Is(err, errProtectedIdentity) {
			t.Fatalf("err = %v, want errProtectedIdentity — the seeded operator is never a target", err)
		}
	})
}

// stubDeprovisioner satisfies identityDeprovisioner and records which saga entry ran, so a
// verb can be proven to drive exactly one of them.
type stubDeprovisioner struct {
	deactivated    string
	purged         string
	resourceTarget agui.DeprovisionTarget
	err            error
}

func (s *stubDeprovisioner) Purge(_ context.Context, target agui.DeprovisionTarget) error {
	s.resourceTarget = target
	return s.err
}

func (s *stubDeprovisioner) Deactivate(_ context.Context, identityID string) error {
	s.deactivated = identityID
	return s.err
}

func (s *stubDeprovisioner) PurgeOne(_ context.Context, identityID string) error {
	s.purged = identityID
	return s.err
}

func TestRunIdentityDeprovision(t *testing.T) {
	const target = "6d0ebdee-bf43-4f5b-abf6-469310d52a5a"

	t.Run("deactivate drives Deactivate and not Purge", func(t *testing.T) {
		stub := &stubDeprovisioner{}
		if err := runIdentityDeprovision(context.Background(), stub, deprovisionVerbDeactivate, target); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if stub.deactivated != target {
			t.Fatalf("Deactivate got %q, want %q", stub.deactivated, target)
		}
		if stub.purged != "" {
			t.Fatalf("PurgeOne was called with %q on a deactivate", stub.purged)
		}
	})

	t.Run("purge drives PurgeOne and not Deactivate", func(t *testing.T) {
		stub := &stubDeprovisioner{}
		if err := runIdentityDeprovision(context.Background(), stub, deprovisionVerbPurge, target); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if stub.purged != target {
			t.Fatalf("PurgeOne got %q, want %q", stub.purged, target)
		}
		if stub.deactivated != "" {
			t.Fatalf("Deactivate was called with %q on a purge", stub.deactivated)
		}
	})

	t.Run("a saga failure propagates", func(t *testing.T) {
		sentinel := errors.New("memory purger is required before identity deletion")
		stub := &stubDeprovisioner{err: sentinel}
		if err := runIdentityDeprovision(context.Background(), stub, deprovisionVerbPurge, target); !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want %v", err, sentinel)
		}
	})

	t.Run("an unknown verb is refused without touching the saga", func(t *testing.T) {
		stub := &stubDeprovisioner{}
		if err := runIdentityDeprovision(context.Background(), stub, deprovisionVerb("evaporate"), target); err == nil {
			t.Fatal("err = nil, want a refusal for an unknown verb")
		}
		if stub.deactivated != "" || stub.purged != "" {
			t.Fatal("the saga was driven by an unknown verb")
		}
	})
}

// TestGuardResumableOrphan pins the resource-plane resumption guard. MEASURED 2026-09-08:
// a purge that fails after the identity_row step leaves resources behind that no CLI could
// reach, because every other path resolves the target through aura.identities — the row
// that is already gone. The saga itself has always accepted an id-only target for exactly
// this; the guard is what keeps that entry from becoming a way around guardProtectedIdentity.
func TestGuardResumableOrphan(t *testing.T) {
	const orphan = "6d0ebdee-bf43-4f5b-abf6-469310d52a5a"

	t.Run("an id with no identity row is resumable", func(t *testing.T) {
		lookup := &fakeIdentityLookup{}
		if err := guardResumableOrphan(context.Background(), lookup, orphan); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})

	t.Run("an id whose identity row still exists is refused", func(t *testing.T) {
		live := identity.Identity{ID: orphan, Name: "b@example.invalid", Kind: "user"}
		lookup := &fakeIdentityLookup{byID: map[string]identity.Identity{orphan: live}}
		err := guardResumableOrphan(context.Background(), lookup, orphan)
		if err == nil {
			t.Fatal("err = nil, want a refusal — a live identity must go through the guarded purge")
		}
		if !strings.Contains(err.Error(), "--resume-resources") {
			t.Fatalf("err = %q, want it to name the flag it is refusing", err)
		}
	})

	t.Run("the seeded local operator is refused even with no row", func(t *testing.T) {
		lookup := &fakeIdentityLookup{}
		if err := guardResumableOrphan(context.Background(), lookup, localSeededIdentityID); !errors.Is(err, errProtectedIdentity) {
			t.Fatalf("err = %v, want errProtectedIdentity", err)
		}
	})

	t.Run("a non-UUID reference is refused", func(t *testing.T) {
		lookup := &fakeIdentityLookup{}
		if err := guardResumableOrphan(context.Background(), lookup, "b@example.invalid"); err == nil {
			t.Fatal("err = nil, want a refusal — a name cannot identify a row that no longer exists")
		}
	})
}

func TestRunIdentityResourcePurge(t *testing.T) {
	const orphan = "6d0ebdee-bf43-4f5b-abf6-469310d52a5a"
	stub := &stubDeprovisioner{}
	if err := runIdentityResourcePurge(context.Background(), stub, orphan); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if stub.resourceTarget.IdentityID != orphan {
		t.Fatalf("Purge target id = %q, want %q", stub.resourceTarget.IdentityID, orphan)
	}
	if stub.resourceTarget.IdentityName != "" || stub.resourceTarget.AuthulaUserID != "" {
		t.Fatalf("Purge target = %+v, want id-only — the identity row and Authula user are already gone, and naming them would re-run steps that cannot succeed", stub.resourceTarget)
	}
	if stub.purged != "" || stub.deactivated != "" {
		t.Fatal("a resource-plane purge drove PurgeOne or Deactivate, which resolve the missing row")
	}
}

func TestIdentityUsageAdvertisesDeprovisionVerbs(t *testing.T) {
	for _, verb := range []string{"deactivate", "purge"} {
		if !strings.Contains(identityUsage, verb) {
			t.Fatalf("identityUsage does not mention %q — the verb is unreachable to an operator reading the usage line", verb)
		}
	}
}
