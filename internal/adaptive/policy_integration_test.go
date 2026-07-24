//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestPolicyServiceBindsEvidenceCapabilityAuditAndDistributedRollback(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	storeA := NewPolicyStore(pool)
	storeB := NewPolicyStore(pool)
	serviceA := NewPolicyService(storeA, "real-production-model")
	serviceB := NewPolicyService(storeB, "real-production-model")
	ctx := context.Background()
	runID := uuid.Must(uuid.NewV7()).String()

	actor := uuid.MustParse("a5a50000-0000-4000-8000-000000000001")
	if _, err := pool.Exec(ctx, `
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, 'adaptive-policy-integration-actor', 'system')
ON CONFLICT (id) DO NOTHING`, actor); err != nil {
		t.Fatalf("seed adaptive manager identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO aura.capability_grants (identity_id, capability)
VALUES ($1, 'adaptive.manage')
ON CONFLICT DO NOTHING`, actor); err != nil {
		t.Fatalf("seed adaptive manager capability: %v", err)
	}

	current, err := storeA.CurrentPolicy(ctx)
	if err != nil {
		t.Fatalf("CurrentPolicy: %v", err)
	}
	if current.Mode != PolicyOff {
		current, err = serviceA.Transition(ctx, actor, current.Epoch, PolicyUpdate{
			Version: "integration-reset-off-" + runID, Mode: PolicyOff, RolloutBPS: 0,
		}, "integration reset")
		if err != nil {
			t.Fatalf("reset off: %v", err)
		}
	}
	shadow, err := serviceA.Transition(ctx, actor, current.Epoch, PolicyUpdate{
		Version: "integration-shadow-" + runID, Mode: PolicyShadow, RolloutBPS: 0,
	}, "start closed shadow cohort")
	if err != nil {
		t.Fatalf("shadow: %v", err)
	}

	candidateVersion := "integration-candidate-" + runID
	cfg := conservativePromotionConfig()
	canaryBatch := eligibleCanaryBatch(650, 850, 10, 10, 1000)
	canaryBatch.ModelID = "real-production-model"
	canaryBatch.PolicyVersion = candidateVersion
	canaryBatch.CohortID = "canary-" + runID
	canary, err := serviceA.Promote(ctx, actor, shadow.Epoch, PolicyUpdate{
		Version: candidateVersion, Mode: PolicyCanary, RolloutBPS: 5000,
	}, canaryBatch, cfg, "calibrated canary gate passed")
	if err != nil {
		t.Fatalf("promote canary: %v", err)
	}

	activeBatch := eligibleCanaryBatch(650, 850, 10, 10, 1000)
	activeBatch.ModelID = "real-production-model"
	activeBatch.PolicyVersion = candidateVersion
	activeBatch.CohortID = "active-" + runID
	active, err := serviceA.Promote(ctx, actor, canary.Epoch, PolicyUpdate{
		Version: candidateVersion, Mode: PolicyActive, RolloutBPS: 10_000,
	}, activeBatch, cfg, "calibrated active gate passed")
	if err != nil {
		t.Fatalf("promote active: %v", err)
	}

	controllerA := NewPolicyController(storeA)
	controllerB := NewPolicyController(storeB)
	before, err := controllerA.Gate(ctx, uuid.Must(uuid.NewV7()))
	if err != nil || !before.Adapt {
		t.Fatalf("process A before rollback = (%+v,%v), want adaptive", before, err)
	}

	rolledBack, err := serviceB.Transition(ctx, actor, active.Epoch, PolicyUpdate{
		Version: "integration-rollback-" + runID, Mode: PolicyRollback, RolloutBPS: 0,
	}, "distributed safety rollback")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	afterA, err := controllerA.Gate(ctx, uuid.Must(uuid.NewV7()))
	if err != nil || afterA.Adapt || afterA.Epoch != rolledBack.Epoch {
		t.Fatalf("process A after rollback = (%+v,%v), want fresh rollback epoch", afterA, err)
	}
	afterB, err := controllerB.Gate(ctx, uuid.Must(uuid.NewV7()))
	if err != nil || afterB.Adapt || afterB.Epoch != rolledBack.Epoch {
		t.Fatalf("process B after rollback = (%+v,%v), want fresh rollback epoch", afterB, err)
	}

	if _, err := serviceA.Transition(ctx, actor, active.Epoch, PolicyUpdate{
		Version: "stale-reactivation-" + runID, Mode: PolicyShadow, RolloutBPS: 0,
	}, "stale transition must fail"); !errors.Is(err, ErrPolicyChanged) {
		t.Fatalf("stale transition error = %v, want ErrPolicyChanged", err)
	}
	var evidence, audits int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_promotion_evidence WHERE policy_version=$1`,
		candidateVersion).Scan(&evidence); err != nil {
		t.Fatalf("count promotion evidence: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_policy_transitions WHERE policy_version=$1`,
		candidateVersion).Scan(&audits); err != nil {
		t.Fatalf("count transition audits: %v", err)
	}
	if evidence != 2 || audits != 2 {
		t.Fatalf("durable gate records = evidence:%d audits:%d, want 2/2", evidence, audits)
	}

	// Leave the shared integration policy safely off.
	if _, err := serviceA.Transition(ctx, actor, rolledBack.Epoch, PolicyUpdate{
		Version: "integration-cleanup-off-" + runID, Mode: PolicyOff, RolloutBPS: 0,
	}, "integration cleanup"); err != nil {
		t.Fatalf("cleanup off: %v", err)
	}
}

func TestPolicyServiceRejectsUnauthorizedActorAndWrongProductionModel(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	store := NewPolicyStore(pool)
	service := NewPolicyService(store, "real-production-model")
	ctx := context.Background()
	actor := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		actor, "adaptive-unauthorized-"+actor.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, actor)
	})
	current, err := store.CurrentPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(ctx, actor, current.Epoch, PolicyUpdate{
		Version: "unauthorized-shadow", Mode: PolicyShadow,
	}, "must deny"); !errors.Is(err, ErrAdaptiveManageDenied) {
		t.Fatalf("unauthorized transition error = %v, want ErrAdaptiveManageDenied", err)
	}

	batch := eligibleCanaryBatch(650, 850, 10, 10, 1000)
	batch.ModelID = "synthetic-model-not-production"
	if _, err := service.Promote(ctx, actor, current.Epoch, PolicyUpdate{
		Version: batch.PolicyVersion, Mode: PolicyCanary, RolloutBPS: 100,
	}, batch, conservativePromotionConfig(), "must deny"); !errors.Is(err, ErrProductionModelMismatch) {
		t.Fatalf("wrong-model promotion error = %v, want ErrProductionModelMismatch", err)
	}
}

func TestAdaptivePolicyRuntimeRejectsRawDMLButAuditedServiceStillTransitions(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	for name, statement := range map[string]string{
		"state update": `UPDATE aura.adaptive_policy_state SET mode='active', rollout_bps=10000
			WHERE scope='global'`,
		"state delete": `DELETE FROM aura.adaptive_policy_state WHERE scope='global'`,
		"state insert": `INSERT INTO aura.adaptive_policy_state
			(scope,epoch,policy_version,mode,rollout_bps,config)
			VALUES ('global',1,'raw','off',0,'{}'::jsonb) ON CONFLICT DO NOTHING`,
		"evidence insert": `INSERT INTO aura.adaptive_promotion_evidence
			(id,actor_id,policy_version,model_id,cohort_id,evidence_hash,report)
			VALUES (gen_random_uuid(),'00000000-0000-0000-0000-000000000001',
			'raw','raw','raw',decode(repeat('00',32),'hex'),'{}'::jsonb)`,
		"audit insert": `INSERT INTO aura.adaptive_policy_transitions
			(id,actor_id,from_epoch,to_epoch,from_mode,to_mode,policy_version,rollout_bps,reason)
			VALUES (gen_random_uuid(),'00000000-0000-0000-0000-000000000001',
			1,2,'off','shadow','raw',0,'raw bypass')`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatalf("runtime raw %s succeeded", name)
		}
	}

	actor := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id,name,kind) VALUES ($1,$2,'user')`,
		actor, "adaptive-policy-hardening-"+actor.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.capability_grants (identity_id,capability) VALUES ($1,'adaptive.manage')`,
		actor); err != nil {
		t.Fatal(err)
	}
	current, err := NewPolicyStore(pool).CurrentPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := NewPolicyService(NewPolicyStore(pool), "real-production-model").Transition(
		ctx, actor, current.Epoch,
		PolicyUpdate{Version: "runtime-hardening-rollback", Mode: PolicyRollback},
		"prove security-definer transition boundary",
	)
	if err != nil {
		t.Fatalf("PolicyService Transition: %v", err)
	}
	if updated.Mode != PolicyRollback || updated.Epoch != current.Epoch+1 {
		t.Fatalf("audited service state = %+v, want rollback epoch %d", updated, current.Epoch+1)
	}
	service := NewPolicyService(NewPolicyStore(pool), "real-production-model")
	shadow, err := service.Transition(ctx, actor, updated.Epoch,
		PolicyUpdate{Version: "runtime-hardening-shadow", Mode: PolicyShadow},
		"prove actor audit survives deletion",
	)
	if err != nil {
		t.Fatalf("PolicyService shadow Transition: %v", err)
	}
	off, err := service.Transition(ctx, actor, shadow.Epoch,
		PolicyUpdate{Version: "runtime-hardening-off", Mode: PolicyOff},
		"return shared policy off before actor deletion",
	)
	if err != nil || off.Mode != PolicyOff {
		t.Fatalf("PolicyService off Transition = (%+v,%v)", off, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id=$1`, actor); err != nil {
		t.Fatalf("delete policy actor: %v", err)
	}
	var retainedAudits int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_policy_transitions WHERE actor_id=$1`, actor,
	).Scan(&retainedAudits); err != nil {
		t.Fatal(err)
	}
	if retainedAudits < 3 {
		t.Fatalf("retained policy audits = %d, want at least 3 after actor deletion", retainedAudits)
	}
}
