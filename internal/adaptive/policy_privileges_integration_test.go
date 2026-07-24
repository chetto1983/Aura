//go:build db_integration

package adaptive

import (
	"context"
	"testing"
)

func TestAdaptivePolicyRuntimeRejectsRawDML(t *testing.T) {
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
}
