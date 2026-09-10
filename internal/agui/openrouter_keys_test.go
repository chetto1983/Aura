package agui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// adminCaps is an identity admin whose listed identities hold identity.create.
func adminCaps(admins ...string) *fakeIdentityAdmin {
	caps := map[string][]string{}
	for _, id := range admins {
		caps[id] = []string{identity.CapIdentityCreate}
	}
	return &fakeIdentityAdmin{caps: caps}
}

type fakeMinting struct {
	mu      sync.Mutex
	keySet  bool
	failFor map[string]error
	minted  []openrouterprovision.MintRequest
	revoked []string
	next    int
}

func (f *fakeMinting) ManagementKeySet(context.Context) (bool, error) { return f.keySet, nil }

func (f *fakeMinting) Mint(_ context.Context, req openrouterprovision.MintRequest) (openrouterprovision.MintResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.minted = append(f.minted, req)
	if err := f.failFor[req.IdentityID]; err != nil {
		return openrouterprovision.MintResult{}, err
	}
	f.next++
	hash := fmt.Sprintf("hash-%d", f.next)
	return openrouterprovision.MintResult{Key: "sk-or-v1-" + hash, Record: openrouterprovision.KeyRecord{Hash: hash, Label: "sk-or-v1-..." + hash}}, nil
}

func (f *fakeMinting) Revoke(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, hash)
	return nil
}

type fakeIdentityKeys struct {
	mu        sync.Mutex
	records   map[string]identitykey.Record
	insertErr error
	// raceWinner is written by "someone else" just before the next InsertIfAbsent runs.
	raceWinner *identitykey.Record
}

func newFakeIdentityKeys() *fakeIdentityKeys {
	return &fakeIdentityKeys{records: map[string]identitykey.Record{}}
}

func (f *fakeIdentityKeys) Load(ctx context.Context) (identitykey.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[identityctx.IdentityID(ctx)]
	if !ok {
		return identitykey.Record{}, identitykey.ErrNoKey
	}
	return rec, nil
}

func (f *fakeIdentityKeys) InsertIfAbsent(ctx context.Context, r identitykey.Record) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return false, f.insertErr
	}
	id := identityctx.IdentityID(ctx)
	if f.raceWinner != nil {
		f.records[id], f.raceWinner = *f.raceWinner, nil
	}
	if _, taken := f.records[id]; taken {
		return false, nil
	}
	f.records[id] = r
	return true, nil
}

func billing() bool { return true }

func TestMinterGivesAnAdminAKeyWithNoLimit(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	minter := NewIdentityKeyMinter(minting, keys, adminCaps("id-admin"), billing)

	minted, err := minter.MintKey(context.Background(), "id-admin", "id-admin")
	if err != nil || minted.Hash != "hash-1" {
		t.Fatalf("MintKey = %+v, %v; want hash-1", minted, err)
	}
	if len(minting.minted) != 1 || minting.minted[0].Limit != nil {
		t.Fatalf("mint requests = %+v, want one with no limit", minting.minted)
	}
	if keys.records["id-admin"].LimitUSD != nil {
		t.Fatal("the admin's stored key has a cap, want none")
	}
}

func TestMinterGivesAMemberAZeroCap(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	if _, err := minter.MintKey(context.Background(), "id-member", "id-member"); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if req := minting.minted[0]; req.Limit == nil || *req.Limit != 0 || req.IdentityID != "id-member" {
		t.Fatalf("mint request = %+v, want a zero cap for id-member", req)
	}
	if got := keys.records["id-member"].LimitUSD; got == nil || *got != 0 {
		t.Fatal("the member's stored key is not at a zero cap")
	}
}

func TestMinterMintsNothingYet(t *testing.T) {
	for name, tc := range map[string]struct{ keySet, routeBills bool }{
		"local route":           {keySet: true, routeBills: false},
		"no management key yet": {keySet: false, routeBills: true},
	} {
		t.Run(name, func(t *testing.T) {
			minting := &fakeMinting{keySet: tc.keySet}
			minter := NewIdentityKeyMinter(minting, newFakeIdentityKeys(), adminCaps(), func() bool { return tc.routeBills })
			minted, err := minter.MintKey(context.Background(), "id", "id")
			if err != nil || minted != (MintedKey{}) || len(minting.minted) != 0 {
				t.Fatalf("MintKey = %+v, %v with %d mints; want nothing minted and no error", minted, err, len(minting.minted))
			}
		})
	}
}

func TestMinterKeepsAnExistingKey(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	keys.records["id"] = identitykey.Record{Key: "sk-existing", Hash: "hash-existing", Label: "existing"}
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	minted, err := minter.MintKey(context.Background(), "id", "id")
	if err != nil || minted.Hash != "hash-existing" || len(minting.minted) != 0 {
		t.Fatalf("MintKey = %+v, %v with %d mints; want the existing key and no mint", minted, err, len(minting.minted))
	}
}

func TestMinterRevokesTheKeyThatLostTheRace(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	keys.raceWinner = &identitykey.Record{Key: "sk-winner", Hash: "hash-winner", Label: "winner"}
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	minted, err := minter.MintKey(context.Background(), "id", "id")
	if err != nil || minted.Hash != "hash-winner" {
		t.Fatalf("MintKey = %+v, %v; want the winner's key", minted, err)
	}
	if len(minting.revoked) != 1 || minting.revoked[0] != "hash-1" {
		t.Fatalf("revoked = %v, want the losing mint hash-1", minting.revoked)
	}
}

func TestMinterRevokesAKeyItCouldNotRecord(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	keys.insertErr = errors.New("store down")
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	if _, err := minter.MintKey(context.Background(), "id", "id"); err == nil {
		t.Fatal("a failed store write passed silently")
	}
	if len(minting.revoked) != 1 || minting.revoked[0] != "hash-1" {
		t.Fatalf("revoked = %v, want the unrecorded hash-1", minting.revoked)
	}
}

func TestMinterRevokeIgnoresAMintThatNeverHappened(t *testing.T) {
	minting := &fakeMinting{keySet: true}
	minter := NewIdentityKeyMinter(minting, newFakeIdentityKeys(), adminCaps(), billing)
	if err := minter.RevokeKey(context.Background(), ""); err != nil || len(minting.revoked) != 0 {
		t.Fatalf("RevokeKey(\"\") = %v with %d revokes; want nil and none", err, len(minting.revoked))
	}
}
