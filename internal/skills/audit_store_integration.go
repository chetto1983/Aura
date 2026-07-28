//go:build db_integration

package skills

import (
	"context"
	"fmt"
)

// InsertAudit appends one audit row via the pool in db_integration builds.
// Production code uses InsertAuditTx so audit rows participate in caller-owned
// transactions.
func (s *AuditStore) InsertAudit(ctx context.Context, in AuditInsert) (AuditRow, error) {
	row, err := s.q.InsertSkillAudit(ctx, in.toParams())
	if err != nil {
		return AuditRow{}, fmt.Errorf("insert skill audit %q: %w", in.SkillName, classifyAuditErr(err))
	}
	return auditFromRow(row), nil
}
