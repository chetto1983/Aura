import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { SkillBodyEditor } from './SkillBodyEditor';
import {
  createSkill,
  updateSkill,
  validateSkill,
  type SkillDraft,
  type SkillValidation,
} from './governanceApi';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';

// SkillEditor — author a skill in the cockpit: Tiptap for the body, plain fields for the
// frontmatter, and the write-boundary verdict live while you type.
//
// The validation is NOT reimplemented here. Every keystroke (debounced) asks the server to
// dry-run the SAME skills.ValidateForWrite the save will run, with the deployment's own
// injection blocklist and body cap — values a browser has no way to know and no business
// guessing. A second copy of the rules in TypeScript would be a copy that drifts, and the
// operator would learn about the drift at save time.
//
// Saving activates. There is no draft state, no pending stage, no approval (amendment
// #97), so the button says what happens and the note says it too.

/** How long the operator must stop typing before the draft is checked. Long enough not to
 * validate mid-word, short enough that the verdict feels attached to the edit. */
const VALIDATE_DEBOUNCE_MS = 400;

export interface SkillEditorProps {
  /** The skill being edited, or undefined to author a new one. */
  readonly initial?: SkillDraft;
  readonly onClose: () => void;
  /** Called after the skill is live, so the host can refresh and select it. */
  readonly onSaved: (name: string) => void;
}

export function SkillEditor({ initial, onClose, onSaved }: SkillEditorProps) {
  const { t } = useTranslation();
  const isNew = initial === undefined;
  const [draft, setDraft] = useState<SkillDraft>(
    initial ?? { name: '', description: '', body: '', always: false },
  );
  const [debounced, setDebounced] = useState<SkillDraft>(draft);
  const [saveError, setSaveError] = useState<string | undefined>(undefined);

  useEffect(() => {
    const id = setTimeout(() => {
      setDebounced(draft);
    }, VALIDATE_DEBOUNCE_MS);
    return () => {
      clearTimeout(id);
    };
  }, [draft]);

  // An untouched new draft is not "invalid", it is empty — checking it would greet the
  // operator with an error before they have typed anything.
  const worthChecking = debounced.name !== '' || debounced.body !== '';
  const validation = useQuery<SkillValidation>({
    queryKey: ['governance', 'skills', 'validate', debounced],
    queryFn: () => validateSkill(debounced),
    retry: false,
    enabled: worthChecking,
  });

  const save = useMutation({
    mutationFn: (next: SkillDraft) => (isNew ? createSkill(next) : updateSkill(next)),
    onSuccess: () => {
      setSaveError(undefined);
      onSaved(draft.name);
    },
    onError: (err: unknown) => {
      setSaveError(err instanceof Error ? err.message : String(err));
    },
  });

  const verdict = worthChecking ? validation.data : undefined;
  const blocked = verdict !== undefined && !verdict.ok;
  const fieldError = (field: string) =>
    blocked && verdict.field === field ? verdict.message : undefined;
  const formError = blocked && (verdict.field ?? '') === '' ? verdict.message : undefined;

  function patch(next: Partial<SkillDraft>) {
    setDraft((current) => ({ ...current, ...next }));
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto">
      <header className="flex flex-wrap items-end justify-between gap-4 border-b border-border px-5 py-4">
        <div>
          <span className="font-mono text-[11.5px] uppercase tracking-[0.13em] text-text-muted">
            {t('governance.skills.editor.eyebrow')}
          </span>
          <h3 className="mt-0.5 font-display text-[22px] font-semibold text-text">
            {isNew ? t('governance.skills.editor.newTitle') : draft.name}
          </h3>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            {t('governance.skills.editor.cancel')}
          </Button>
          <Button
            type="button"
            disabled={blocked || save.isPending || draft.name === ''}
            aria-busy={save.isPending ? 'true' : undefined}
            onClick={() => {
              save.mutate(draft);
            }}
          >
            {t('governance.skills.editor.save')}
          </Button>
        </div>
      </header>

      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,7fr)_minmax(0,4fr)]">
        <div className="min-w-0 border-b border-border lg:border-b-0 lg:border-r">
          <SkillBodyEditor
            value={initial?.body ?? ''}
            onChange={(body) => {
              patch({ body });
            }}
            label={t('governance.skills.editor.bodyLabel')}
          />
        </div>

        <aside className="flex flex-col gap-4 p-5">
          {/* Label and hint are separate: htmlFor names the control, aria-describedby
              attaches the guidance. Nesting the hint inside the <label> would fold it into
              the accessible name, so a screen reader would announce the whole paragraph
              every time focus lands on the field. */}
          <div className="flex flex-col gap-1.5">
            <label htmlFor="skill-name" className="text-[13px] font-semibold text-text">
              {t('governance.skills.editor.nameLabel')}
            </label>
            <Input
              id="skill-name"
              value={draft.name}
              readOnly={!isNew}
              spellCheck={false}
              aria-describedby="skill-name-hint"
              aria-invalid={fieldError('name') !== undefined}
              onChange={(e) => {
                patch({ name: e.target.value });
              }}
              className="font-mono"
            />
            <span id="skill-name-hint" className="text-[12px] text-text-muted">
              {isNew
                ? t('governance.skills.editor.nameHint')
                : t('governance.skills.editor.nameLocked')}
            </span>
            {fieldError('name') !== undefined ? (
              <span role="alert" className="text-[12.5px] text-danger">
                {fieldError('name')}
              </span>
            ) : null}
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="skill-description" className="text-[13px] font-semibold text-text">
              {t('governance.skills.editor.descriptionLabel')}
            </label>
            <Textarea
              id="skill-description"
              rows={3}
              value={draft.description}
              aria-describedby="skill-description-hint"
              onChange={(e) => {
                patch({ description: e.target.value });
              }}
            />
            <span id="skill-description-hint" className="text-[12px] text-text-muted">
              {t('governance.skills.editor.descriptionHint')}
            </span>
          </div>

          <div className="flex items-start gap-2.5">
            <Checkbox
              id="skill-always"
              checked={draft.always}
              aria-describedby="skill-always-hint"
              onCheckedChange={(checked) => {
                patch({ always: checked === true });
              }}
              className="mt-0.5"
            />
            <span>
              <label htmlFor="skill-always" className="block text-[13px] font-semibold text-text">
                {t('governance.skills.editor.alwaysLabel')}
              </label>
              <span id="skill-always-hint" className="block text-[12px] text-text-muted">
                {t('governance.skills.editor.alwaysHint')}
              </span>
            </span>
          </div>

          {formError !== undefined ? (
            <p
              role="alert"
              className="rounded-md border border-danger bg-danger/10 px-3 py-2 text-[13px] text-danger"
            >
              {formError}
            </p>
          ) : null}
          {fieldError('body') !== undefined ? (
            <p
              role="alert"
              className="rounded-md border border-danger bg-danger/10 px-3 py-2 text-[13px] text-danger"
            >
              {fieldError('body')}
            </p>
          ) : null}
          {saveError !== undefined ? (
            <p
              role="alert"
              className="rounded-md border border-danger bg-danger/10 px-3 py-2 text-[13px] text-danger"
            >
              {t('governance.skills.editor.saveError')}
            </p>
          ) : null}

          <p className="rounded-r-md border border-l-[3px] border-border border-l-success bg-surface-2 px-3.5 py-2.5 text-[13px] text-text-muted">
            {t('governance.skills.editor.liveNote')}
          </p>
        </aside>
      </div>
    </div>
  );
}
