import { useTranslation } from 'react-i18next';
import type { AuditRow } from './governanceApi';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';

// SkillsAuditList — the append-only ledger, newest-first. Extracted from SkillsBoard when
// the audit tab became a toggle (amendment #97). The ledger keeps its pre-#97 rows, so
// retired actions still render: the ledger records what happened, not what the code
// currently does.

export interface SkillsAuditListProps {
  readonly rows: readonly AuditRow[];
}

export function SkillsAuditList({ rows }: SkillsAuditListProps) {
  const { t } = useTranslation();
  const ordered = [...rows].sort((a, b) => b.CreatedAt.localeCompare(a.CreatedAt));

  return (
    <ul aria-label={t('governance.skills.stages.audit')} className="flex flex-col gap-2 p-2">
      {ordered.map((row) => (
        <Card key={row.ID} role="listitem" className="gap-2 p-3">
          <span className="flex items-center justify-between gap-2">
            <span className="break-words text-[15.5px] font-semibold text-text">
              {row.SkillName}
            </span>
            <Badge variant="secondary">{row.Action}</Badge>
          </span>
          <span className="flex items-center justify-between gap-2 text-[13px] text-text-muted">
            <span className="font-mono">{row.CreatedAt}</span>
            <span className="break-all">{row.ActorID}</span>
          </span>
        </Card>
      ))}
    </ul>
  );
}
