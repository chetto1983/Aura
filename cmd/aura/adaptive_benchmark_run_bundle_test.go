package main

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

func TestBuildAdaptiveBenchmarkDecisionBundleFreezesTenOwnerDomains(
	t *testing.T,
) {
	t.Parallel()
	runID := uuid.Must(uuid.NewV7())
	ownerID := uuid.Must(uuid.NewV7())
	observed := adaptive.Policy{
		Epoch: 42, Version: "policy-42",
		Mode: adaptive.PolicyShadow, RolloutBPS: 0,
		Config:    json.RawMessage(`{"domain":"reasoning"}`),
		UpdatedAt: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
	}
	first, err := buildAdaptiveBenchmarkDecisionBundle(
		runID,
		ownerID,
		"llamacpp",
		"qwen",
		observed,
	)
	if err != nil {
		t.Fatalf("buildAdaptiveBenchmarkDecisionBundle: %v", err)
	}
	second, err := buildAdaptiveBenchmarkDecisionBundle(
		runID,
		ownerID,
		"llamacpp",
		"qwen",
		observed,
	)
	if err != nil {
		t.Fatalf("repeat buildAdaptiveBenchmarkDecisionBundle: %v", err)
	}
	if first.ID() == uuid.Nil ||
		first.SHA256() == "" ||
		first.ID() != second.ID() ||
		first.SHA256() != second.SHA256() ||
		!bytes.Equal(first.ManifestJSON(), second.ManifestJSON()) {
		t.Fatal("offline decision bundle identity is not deterministic")
	}
	if first.PrimaryOwnerID() != ownerID ||
		first.ControlOwnerID() !=
			uuid.MustParse(identityctx.CLIServiceIdentity) {
		t.Fatal("offline decision bundle owners drifted")
	}
	entries := first.Entries()
	catalogs := auraeval.AdaptiveBenchmarkActionCatalogs()
	controlOwnerID := uuid.MustParse(identityctx.CLIServiceIdentity)
	owners := []uuid.UUID{ownerID, controlOwnerID}
	if len(entries) != len(catalogs)*len(owners) {
		t.Fatalf(
			"entries=%d want=%d",
			len(entries),
			len(catalogs)*len(owners),
		)
	}
	for index, entry := range entries {
		owner := owners[index/len(catalogs)]
		catalog := catalogs[index%len(catalogs)]
		if entry.OwnerID != owner ||
			entry.Domain != catalog.Domain ||
			entry.Snapshot == nil ||
			entry.Snapshot.ID() != entry.SnapshotID ||
			entry.Snapshot.SHA256() != entry.SnapshotSHA256 {
			t.Fatalf("entry %d drifted: %#v", index, entry)
		}
		actions := entry.Snapshot.Actions()
		actionIDs := make([]string, len(actions))
		for actionIndex, action := range actions {
			actionIDs[actionIndex] = action.ActionID
			if action.Examples == nil || len(action.Examples) != 0 {
				t.Fatalf(
					"domain %s action %s has noncanonical support",
					entry.Domain,
					action.ActionID,
				)
			}
		}
		if !slices.Equal(actionIDs, catalog.ActionIDs) {
			t.Fatalf(
				"domain %s actions=%v want=%v",
				entry.Domain,
				actionIDs,
				catalog.ActionIDs,
			)
		}
		policy, err := first.PolicyReader(
			entry.OwnerID,
			entry.Domain,
		).CurrentPolicy(
			t.Context(),
		)
		if err != nil ||
			policy.Mode != adaptive.PolicyShadow ||
			policy.RolloutBPS != 0 ||
			policy.Epoch != observed.Epoch ||
			policy.Version != observed.Version {
			t.Fatalf("domain %s policy=%#v error=%v", entry.Domain, policy, err)
		}
		loaded, err := first.Load(
			t.Context(),
			entry.OwnerID,
			entry.SnapshotID,
		)
		if err != nil || loaded.SHA256() != entry.SnapshotSHA256 {
			t.Fatalf("domain %s load=%v error=%v", entry.Domain, loaded, err)
		}
	}
	var manifest adaptiveBenchmarkDecisionBundleManifest
	if err := json.Unmarshal(first.ManifestJSON(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaID !=
		"aura.adaptive.benchmark-decision-bundle/v2" ||
		manifest.Revision != 2 ||
		manifest.PrimaryOwnerID != ownerID.String() ||
		manifest.ControlOwnerID != controlOwnerID.String() ||
		len(manifest.Entries) != 10 {
		t.Fatalf("manifest=%#v", manifest)
	}
}

func TestBuildAdaptiveBenchmarkDecisionBundleRejectsScopeAndPolicyDrift(
	t *testing.T,
) {
	t.Parallel()
	validPolicy := adaptive.Policy{
		Epoch: 1, Version: "policy-1",
		Mode: adaptive.PolicyOff, RolloutBPS: 0,
		Config:    json.RawMessage(`{}`),
		UpdatedAt: time.Now().UTC(),
	}
	tests := []struct {
		name     string
		runID    uuid.UUID
		ownerID  uuid.UUID
		provider string
		model    string
		policy   adaptive.Policy
	}{
		{name: "missing run", ownerID: uuid.New(), provider: "p", model: "m", policy: validPolicy},
		{name: "missing owner", runID: uuid.New(), provider: "p", model: "m", policy: validPolicy},
		{
			name:  "control owner reused as primary",
			runID: uuid.New(),
			ownerID: uuid.MustParse(
				identityctx.CLIServiceIdentity,
			),
			provider: "p",
			model:    "m",
			policy:   validPolicy,
		},
		{name: "missing provider", runID: uuid.New(), ownerID: uuid.New(), model: "m", policy: validPolicy},
		{name: "missing model", runID: uuid.New(), ownerID: uuid.New(), provider: "p", policy: validPolicy},
		{
			name: "invalid epoch", runID: uuid.New(), ownerID: uuid.New(),
			provider: "p", model: "m",
			policy: changedAdaptiveBenchmarkPolicy(
				validPolicy,
				func(policy *adaptive.Policy) { policy.Epoch = 0 },
			),
		},
		{
			name: "invalid config", runID: uuid.New(), ownerID: uuid.New(),
			provider: "p", model: "m",
			policy: changedAdaptiveBenchmarkPolicy(
				validPolicy,
				func(policy *adaptive.Policy) {
					policy.Config = json.RawMessage(`{`)
				},
			),
		},
		{
			name: "invalid time", runID: uuid.New(), ownerID: uuid.New(),
			provider: "p", model: "m",
			policy: changedAdaptiveBenchmarkPolicy(
				validPolicy,
				func(policy *adaptive.Policy) {
					policy.UpdatedAt = time.Time{}
				},
			),
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if bundle, err := buildAdaptiveBenchmarkDecisionBundle(
				testCase.runID,
				testCase.ownerID,
				testCase.provider,
				testCase.model,
				testCase.policy,
			); err == nil {
				t.Fatalf("accepted bundle %#v", bundle)
			}
		})
	}
}

func TestAdaptiveBenchmarkDecisionBundleLoaderRejectsCrossOwnerAndUnknownSnapshot(
	t *testing.T,
) {
	t.Parallel()
	ownerID := uuid.Must(uuid.NewV7())
	controlOwnerID := uuid.MustParse(identityctx.CLIServiceIdentity)
	bundle, err := buildAdaptiveBenchmarkDecisionBundle(
		uuid.Must(uuid.NewV7()),
		ownerID,
		"openrouter",
		"model",
		adaptive.Policy{
			Epoch: 2, Version: "policy-2",
			Mode: adaptive.PolicyShadow, Config: json.RawMessage(`{}`),
			UpdatedAt: time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf("buildAdaptiveBenchmarkDecisionBundle: %v", err)
	}
	for _, input := range []struct {
		ownerID  uuid.UUID
		snapshot uuid.UUID
	}{
		{
			ownerID:  controlOwnerID,
			snapshot: bundle.Entries()[0].SnapshotID,
		},
		{
			ownerID:  uuid.New(),
			snapshot: bundle.Entries()[0].SnapshotID,
		},
		{ownerID: ownerID, snapshot: uuid.New()},
	} {
		if snapshot, err := bundle.Load(
			context.Background(),
			input.ownerID,
			input.snapshot,
		); err == nil {
			t.Fatalf("accepted snapshot %#v", snapshot)
		}
	}
}

func changedAdaptiveBenchmarkPolicy(
	policy adaptive.Policy,
	change func(*adaptive.Policy),
) adaptive.Policy {
	change(&policy)
	return policy
}
