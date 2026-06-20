//go:build db_integration

package identity

import (
	"context"
	"fmt"
)

// InsertIdentityAudit appends one audit row via the pool in db_integration builds.
// Production code uses InsertIdentityAuditTx so audit rows participate in the
// provisioning saga's transaction.
func (s *IdentityAuditStore) InsertIdentityAudit(ctx context.Context, in IdentityAuditInsert) (IdentityAuditRow, error) {
	params, err := in.toParams()
	if err != nil {
		return IdentityAuditRow{}, fmt.Errorf("insert identity audit: %w", err)
	}
	row, err := s.q.InsertIdentityAudit(ctx, params)
	if err != nil {
		return IdentityAuditRow{}, fmt.Errorf("insert identity audit %q: %w", in.NewIdentityName, classifyAuditErr(err))
	}
	return identityAuditFromRow(row), nil
}
