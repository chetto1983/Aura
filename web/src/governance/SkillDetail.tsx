import { useTranslation } from 'react-i18next';
import type { SkillRow } from './governanceApi';

// SkillDetail — the detail pane for a selected skill (the NodeInspector <dl> idiom). It renders
// the skill metadata (type / language / content hash / description) as React-escaped text, mono
// for the data-shaped values (language, content hash). A missing field renders as "—" (never
// fabricated). CRITICALLY there is NO run/activate/install control here — the board is read-only
// (T-28-03-02 / GOV-02); a pending skill additionally shows the "inactive and cannot be run"
// note. The close button restores focus to the originating row.

export interface SkillDetailProps {
  readonly skill: SkillRow;
  readonly isPending: boolean;
  readonly onClose: () => void;
}

export function SkillDetail({ skill, isPending, onClose }: SkillDetailProps) {
  const { t } = useTranslation();
  const dash = t('governance.skills.field.none');

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-scroll overscroll-contain p-4 [scrollbar-gutter:stable]">
      <header className="flex items-start justify-between gap-2">
        <h3 className="break-words font-display text-[18px] font-semibold text-text">
          {skill.name}
        </h3>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="min-h-[44px] min-w-[44px] rounded-md text-text-muted hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          ✕
        </button>
      </header>

      {isPending ? (
        <p
          role="note"
          className="rounded-md border border-border bg-surface-2 px-3 py-2 text-[13px] text-warning"
        >
          {t('governance.skills.pendingNote')}
        </p>
      ) : null}

      <dl className="flex flex-col gap-3">
        <div className="flex flex-col">
          <dt className="text-[13px] font-semibold text-text-muted">
            {t('governance.skills.field.type')}
          </dt>
          <dd className="text-[15.5px] text-text">{skill.type !== '' ? skill.type : dash}</dd>
        </div>
        <div className="flex flex-col">
          <dt className="text-[13px] font-semibold text-text-muted">
            {t('governance.skills.field.language')}
          </dt>
          <dd className="font-mono text-[13px] text-text">
            {skill.language !== undefined && skill.language !== '' ? skill.language : dash}
          </dd>
        </div>
        <div className="flex flex-col">
          <dt className="text-[13px] font-semibold text-text-muted">
            {t('governance.skills.field.contentHash')}
          </dt>
          <dd className="break-all font-mono text-[13px] text-text">
            {skill.contentHash !== undefined && skill.contentHash !== '' ? skill.contentHash : dash}
          </dd>
        </div>
        <div className="flex flex-col">
          <dt className="text-[13px] font-semibold text-text-muted">
            {t('governance.skills.field.description')}
          </dt>
          <dd className="break-words text-[15.5px] leading-relaxed text-text">
            {skill.description !== '' ? skill.description : dash}
          </dd>
        </div>
      </dl>
    </div>
  );
}
