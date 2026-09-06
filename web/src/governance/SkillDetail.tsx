import { useQuery } from '@tanstack/react-query';
import { Pencil, Trash2, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import { baseMarkdownComponents, buildRehypePlugins, remarkPlugins } from '../chat/markdownConfig';
import { fetchSkillBody, type SkillDraft, type SkillRow, type SkillStage } from './governanceApi';
import { Button } from '@/components/ui/button';

// SkillDetail — the detail pane for a selected skill: the metadata strip, then the SKILL
// BODY, then the delete affordance.
//
// The body is the point. The pane used to show type, language, hash and description —
// everything except what the skill tells the agent to do, which is the one thing you open
// it for. It renders through the SAME sanitize chokepoint as the chat lane
// (markdownConfig / HARDEN-08): a skill body is operator-authored but can arrive from an
// npx-installed third-party repo, so it is never trusted HTML.
//
// Only an ACTIVE body is fetchable — the route resolves through the loader snapshot, so an
// archived skill shows its metadata and an explicit note instead of a body.

export interface SkillDetailProps {
  readonly skill: SkillRow;
  readonly stage: SkillStage;
  readonly onClose: () => void;
  /** Opens the delete confirm for this skill. */
  readonly onDelete: (name: string) => void;
  /** Opens the editor on this skill, seeded with the body already loaded here. */
  readonly onEdit: (draft: SkillDraft) => void;
}

export function SkillDetail({ skill, stage, onClose, onDelete, onEdit }: SkillDetailProps) {
  const { t } = useTranslation();
  const dash = t('governance.skills.field.none');
  const isActive = stage === 'active';

  const body = useQuery({
    queryKey: ['governance', 'skills', 'body', skill.name],
    queryFn: () => fetchSkillBody(skill.name),
    retry: false,
    enabled: isActive,
  });

  return (
    // No scroll container here on purpose: BoardLayout's <aside> owns the scrolling for
    // all three boards. This pane used to declare its own (h-full + overflow-y-scroll +
    // overscroll-contain) and it worked only on lg, where the grid gives the aside a
    // definite height. On the mobile sheet the aside is fixed with max-h, so its height is
    // auto, so h-full here resolved to auto too: the pane had nothing to scroll, while
    // overscroll-contain still blocked the wheel/touch from chaining up to the aside that
    // COULD scroll. The visible bar dragged, the content did not move.
    <div className="flex flex-col gap-4 p-4">
      <header className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="break-words font-display text-[20px] font-semibold text-text">
            {skill.name}
          </h3>
          {skill.description !== '' ? (
            <p className="mt-1 break-words text-[14.5px] leading-relaxed text-text-muted">
              {skill.description}
            </p>
          ) : null}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="text-text-muted hover:text-text"
        >
          <X data-icon aria-hidden="true" className="size-4" />
        </Button>
      </header>

      <dl className="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border sm:grid-cols-4">
        <SkillFact label={t('governance.skills.field.type')} value={skill.type} fallback={dash} />
        <SkillFact
          label={t('governance.skills.field.state')}
          value={t(`governance.skills.filter.${stage}`)}
          fallback={dash}
        />
        <SkillFact
          label={t('governance.skills.field.language')}
          value={skill.language ?? ''}
          fallback={dash}
          mono
        />
        <SkillFact
          label={t('governance.skills.field.contentHash')}
          value={skill.contentHash ?? ''}
          fallback={dash}
          mono
        />
      </dl>

      {skill.always === true ? (
        <p
          role="note"
          className="rounded-md border border-info bg-info/10 px-3 py-2 text-[13px] text-text-muted"
        >
          {t('governance.skills.alwaysNote')}
        </p>
      ) : null}

      <section className="min-w-0 border-t border-border pt-4">
        <h4 className="mb-2 font-display text-[16.5px] font-semibold text-text">
          {t('governance.skills.bodyHeading')}
        </h4>
        <SkillBody
          isActive={isActive}
          isLoading={body.isLoading}
          isError={body.isError}
          text={body.data ?? ''}
        />
      </section>

      <footer className="mt-auto flex justify-end gap-2 border-t border-border pt-4">
        {/* Editing loads the body into the editor, so it is offered only once the body is
            here — an "Edit" that opened on an empty document would quietly propose
            replacing the skill with nothing. */}
        {isActive && body.isSuccess && skill.owned ? (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              onEdit({
                name: skill.name,
                description: skill.description,
                body: body.data,
                always: skill.always === true,
              });
            }}
          >
            <Pencil data-icon="inline-start" aria-hidden="true" focusable="false" />
            {t('governance.skills.editor.edit')}
          </Button>
        ) : null}
        {/* Edit and Delete are offered only on a skill this caller owns. A house skill and
            one shared with them are readable here — that is the point of the pane — but
            both verbs would land on a root skills.Writer.For(identity) cannot address, so
            offering them would promise something the server has to refuse. */}
        <Button
          type="button"
          variant="ghost"
          disabled={!skill.owned}
          title={skill.owned ? undefined : t('governance.skills.houseReadOnly')}
          onClick={() => {
            onDelete(skill.name);
          }}
          className="text-danger hover:bg-danger/15 hover:text-danger"
        >
          <Trash2 data-icon="inline-start" aria-hidden="true" focusable="false" />
          {t('governance.skills.delete.action')}
        </Button>
      </footer>
    </div>
  );
}

/** One metadata cell. A missing value renders the dash placeholder, never a fabrication. */
function SkillFact({
  label,
  value,
  fallback,
  mono = false,
}: {
  readonly label: string;
  readonly value: string;
  readonly fallback: string;
  readonly mono?: boolean;
}) {
  return (
    <div className="bg-surface-2 px-3 py-2">
      <dt className="font-mono text-[10px] uppercase tracking-[0.09em] text-text-muted">{label}</dt>
      <dd
        className={`mt-0.5 break-all text-[13.5px] text-text ${mono ? 'font-mono tabular-nums' : ''}`}
      >
        {value !== '' ? value : fallback}
      </dd>
    </div>
  );
}

/** The body region: the sanitized markdown, or the honest reason there is none to show. */
function SkillBody({
  isActive,
  isLoading,
  isError,
  text,
}: {
  readonly isActive: boolean;
  readonly isLoading: boolean;
  readonly isError: boolean;
  readonly text: string;
}) {
  const { t } = useTranslation();

  if (!isActive) {
    return <p className="text-[14px] text-text-muted">{t('governance.skills.bodyArchived')}</p>;
  }
  if (isLoading) {
    return <p className="text-[14px] text-text-muted">{t('governance.skills.bodyLoading')}</p>;
  }
  if (isError) {
    return (
      <p role="alert" className="text-[14px] text-danger">
        {t('governance.skills.bodyError')}
      </p>
    );
  }
  if (text.trim() === '') {
    return <p className="text-[14px] text-text-muted">{t('governance.skills.bodyEmpty')}</p>;
  }
  return (
    <div className="min-w-0 text-[14.5px] leading-relaxed text-text-muted [&_code]:rounded-[5px] [&_code]:border [&_code]:border-border [&_code]:bg-surface-2 [&_code]:px-1.5 [&_code]:font-mono [&_code]:text-[12.5px] [&_h1]:mb-1.5 [&_h1]:mt-5 [&_h1]:font-display [&_h1]:text-[17px] [&_h1]:font-semibold [&_h1]:text-text [&_h2]:mb-1.5 [&_h2]:mt-5 [&_h2]:font-display [&_h2]:text-[16px] [&_h2]:font-semibold [&_h2]:text-text [&_h3]:mb-1 [&_h3]:mt-4 [&_h3]:font-semibold [&_h3]:text-text [&_li]:my-1 [&_ol]:my-2 [&_ol]:list-decimal [&_ol]:pl-5 [&_p]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:border [&_pre]:border-border [&_pre]:bg-surface-2 [&_pre]:p-3 [&_ul]:my-2 [&_ul]:list-disc [&_ul]:pl-5">
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={buildRehypePlugins()}
        components={baseMarkdownComponents}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
