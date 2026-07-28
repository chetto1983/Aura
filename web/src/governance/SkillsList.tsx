import { Archive, RotateCcw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { SkillRow, SkillStage } from './governanceApi';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';

// SkillsList — the merged master list: active and archived rows in one place, told apart
// by their tags and by which lifecycle verb sits on the row.
//
// Each row carries both verbs: archive (reversible, fires immediately) and delete (opens
// the confirm). Delete started out in the detail only, on the theory that distance protects
// an irreversible action. It does not — it just reads as "you cannot delete this" on a
// board of a dozen skills. The confirm is the protection; the two icons differ in weight
// (a muted archive box, a delete that turns danger-red on hover) rather than in reach.

/** One list entry: the wire row plus the stage it was listed under. */
export interface StagedSkill extends SkillRow {
  readonly stage: SkillStage;
}

export interface SkillsListProps {
  readonly rows: readonly StagedSkill[];
  readonly selected: string | undefined;
  readonly onSelect: (name: string, origin: HTMLElement | null) => void;
  readonly onArchive: (name: string) => void;
  readonly onRestore: (name: string) => void;
  /** Opens the delete confirm. The row never deletes on its own. */
  readonly onDelete: (name: string) => void;
  readonly archivePending: boolean;
  readonly restorePending: boolean;
  /** The archived row whose restore collided with an active skill of the same name. */
  readonly collisionName: string | undefined;
}

export function SkillsList({
  rows,
  selected,
  onSelect,
  onArchive,
  onRestore,
  onDelete,
  archivePending,
  restorePending,
  collisionName,
}: SkillsListProps) {
  const { t } = useTranslation();
  const archiveLabel = t('governance.skills.archive');
  const restoreLabel = t('governance.skills.restore');
  const deleteLabel = t('governance.skills.deleteSkill');

  return (
    <div role="list" aria-label={t('governance.tabs.skills')} className="flex flex-col gap-2 p-2">
      {rows.map((skill) => {
        const isSelected = selected === skill.name;
        const isArchived = skill.stage === 'archived';
        const mutedTone = isSelected ? 'text-on-accent/80' : 'text-text-muted';
        const selectedBadgeClass = isSelected
          ? 'border-on-accent/30 bg-on-accent/15 text-on-accent'
          : undefined;

        return (
          <Card
            key={`${skill.stage}:${skill.name}`}
            role="listitem"
            className={`gap-0 overflow-hidden p-0 transition-colors ${
              isSelected ? 'border-accent' : 'hover:border-border-strong'
            }`}
          >
            <div className="flex min-w-0 items-stretch gap-2">
              <Button
                type="button"
                variant={isSelected ? 'default' : 'ghost'}
                aria-pressed={isSelected}
                onClick={(e) => {
                  onSelect(skill.name, e.currentTarget);
                }}
                // whitespace-normal is load-bearing: the Button base sets
                // whitespace-nowrap, which keeps a long skill name on one line, lets it
                // overflow its box, and draws the badges straight over the text.
                className={`h-auto min-h-[52px] min-w-0 flex-1 flex-col items-stretch justify-start gap-1 whitespace-normal rounded-r-sm px-3 py-2 text-left ${
                  isSelected ? 'text-on-accent' : 'bg-transparent text-text hover:bg-surface-2'
                } ${isArchived && !isSelected ? 'opacity-70' : ''}`}
              >
                <span className="flex w-full min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="min-w-0 break-words text-[15.5px] font-semibold">
                    {skill.name}
                  </span>
                  <Badge variant="secondary" className={selectedBadgeClass}>
                    {skill.type}
                  </Badge>
                  {skill.always === true ? (
                    <Badge variant="info" className={selectedBadgeClass}>
                      {t('governance.skills.alwaysTag')}
                    </Badge>
                  ) : null}
                  {isArchived ? (
                    <Badge variant="secondary" className={selectedBadgeClass}>
                      {t('governance.skills.filter.archived')}
                    </Badge>
                  ) : null}
                </span>
                {skill.description !== '' ? (
                  <span className={`line-clamp-1 w-full break-words text-[13px] ${mutedTone}`}>
                    {skill.description}
                  </span>
                ) : null}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={isArchived ? restoreLabel : archiveLabel}
                title={isArchived ? restoreLabel : archiveLabel}
                disabled={isArchived ? restorePending : archivePending}
                onClick={() => {
                  if (isArchived) {
                    onRestore(skill.name);
                  } else {
                    onArchive(skill.name);
                  }
                }}
                className={`m-1 disabled:cursor-wait ${
                  isArchived
                    ? 'text-accent-text hover:bg-surface-3 hover:text-accent-text'
                    : 'text-text-muted hover:bg-surface-2 hover:text-text'
                }`}
              >
                {isArchived ? (
                  <RotateCcw data-icon="icon" aria-hidden="true" focusable="false" />
                ) : (
                  <Archive data-icon="icon" aria-hidden="true" focusable="false" />
                )}
              </Button>
              {/* Delete sits on the row too, not only in the detail: on a board of a dozen
                  skills, making the irreversible verb reachable only after opening a pane
                  reads as "you cannot delete this". The confirm is what protects it — the
                  distance never did. */}
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={deleteLabel}
                title={deleteLabel}
                onClick={() => {
                  onDelete(skill.name);
                }}
                className="my-1 mr-1 text-text-muted hover:bg-danger/15 hover:text-danger"
              >
                <Trash2 data-icon="icon" aria-hidden="true" focusable="false" />
              </Button>
            </div>
            {collisionName === skill.name ? (
              <p role="alert" className="px-3 pb-2 text-[13px] text-danger">
                {t('governance.skills.collidingRestore', { name: skill.name })}
              </p>
            ) : null}
          </Card>
        );
      })}
    </div>
  );
}
