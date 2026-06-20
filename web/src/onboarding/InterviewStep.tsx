import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { OnboardingAnswers, OnboardingStepResponse } from './onboardingApi';

// InterviewStep (ONBD-02 / D-03) — drives the 5-step LoopAgent over /step. While the session
// status is 'active' it renders the current question (backend-supplied content) + a free-text
// answer field; the Continue CTA POSTs intent 'answer'. Once a draft exists (status 'draft') it
// renders the draft for review with confirm / edit / skip:
//   - confirm  → POST intent 'confirm'  (advances toward provisioning)
//   - edit     → reveals a structured edit form; submit POSTs intent 'edit'; the backend
//                re-renders the draft from the SAME accumulated answers (NO re-prompt, NO second
//                LLM turn — the no-duplicate-LLM-turn guarantee), so the draft simply re-renders.
//   - skip     → POST intent 'skip'; the interview ends without writing a profile.
// Backend content/draft are rendered as React-escaped text (never dangerouslySetInnerHTML,
// T-28-06-05). The wizard owns the session + the loading/error state; this step is presentational.

export interface InterviewStepProps {
  readonly step: OnboardingStepResponse;
  readonly busy: boolean;
  readonly onAnswer: (text: string) => void;
  readonly onConfirm: () => void;
  readonly onEdit: (answers: OnboardingAnswers) => void;
  readonly onSkip: () => void;
}

const isDraft = (step: OnboardingStepResponse): boolean =>
  step.status === 'draft' || (step.draft ?? '').trim() !== '';

export function InterviewStep({
  step,
  busy,
  onAnswer,
  onConfirm,
  onEdit,
  onSkip,
}: InterviewStepProps) {
  const { t } = useTranslation();
  const answerId = useId();
  const [answer, setAnswer] = useState('');
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [editRole, setEditRole] = useState('');

  const draftMode = isDraft(step);

  function submitAnswer() {
    onAnswer(answer);
    setAnswer('');
  }

  function submitEdit() {
    const name = editName.trim();
    const role = editRole.trim();
    const answers: OnboardingAnswers = {
      ...(name !== '' ? { name } : {}),
      ...(role !== '' ? { role } : {}),
    };
    onEdit(answers);
    setEditing(false);
    setEditName('');
    setEditRole('');
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Backend-supplied prompt/transition content — React-escaped prose. */}
      <p className="text-[15.5px] leading-relaxed text-text">
        {step.content.trim() === '' ? t('onboarding.interview.emptyAnswer') : step.content}
      </p>

      {draftMode ? (
        <div className="flex flex-col gap-4">
          <h2 className="font-display text-[18px] font-semibold text-text">
            {t('onboarding.interview.draftHeading')}
          </h2>
          {/* The Agent.md draft — backend text, mono, React-escaped, scroll-capped. */}
          <pre className="max-h-[40svh] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-surface-2 p-4 font-mono text-[13px] leading-relaxed text-text">
            {step.draft ?? ''}
          </pre>

          {editing ? (
            <div className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4">
              <div className="flex flex-col gap-1">
                <label htmlFor={`${answerId}-name`} className="text-[13px] font-semibold text-text">
                  {t('onboarding.credentials.emailLabel')}
                </label>
                <input
                  id={`${answerId}-name`}
                  type="text"
                  value={editName}
                  onChange={(e) => {
                    setEditName(e.target.value);
                  }}
                  className="min-h-[44px] rounded-md border border-border bg-surface-2 px-3 py-2 text-[15.5px] text-text outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <div className="flex flex-col gap-1">
                <label htmlFor={`${answerId}-role`} className="text-[13px] font-semibold text-text">
                  {t('onboarding.steps.interview')}
                </label>
                <input
                  id={`${answerId}-role`}
                  type="text"
                  value={editRole}
                  onChange={(e) => {
                    setEditRole(e.target.value);
                  }}
                  className="min-h-[44px] rounded-md border border-border bg-surface-2 px-3 py-2 text-[15.5px] text-text outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <button
                type="button"
                disabled={busy}
                onClick={submitEdit}
                className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
              >
                {t('onboarding.cta.edit')}
              </button>
            </div>
          ) : null}

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              disabled={busy}
              onClick={onConfirm}
              className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              {t('onboarding.cta.confirm')}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                setEditing((v) => !v);
              }}
              className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none transition-colors hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              {t('onboarding.cta.edit')}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={onSkip}
              className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text-muted outline-none transition-colors hover:border-border-strong hover:text-text focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              {t('onboarding.cta.skip')}
            </button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <label htmlFor={answerId} className="text-[13px] font-semibold text-text">
              {t('onboarding.interview.answerLabel')}
            </label>
            <textarea
              id={answerId}
              rows={4}
              value={answer}
              placeholder={t('onboarding.interview.answerPlaceholder')}
              onChange={(e) => {
                setAnswer(e.target.value);
              }}
              className="rounded-md border border-border bg-surface-2 px-3 py-2 text-[15.5px] leading-relaxed text-text outline-none transition-colors placeholder:text-text-faint focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              disabled={busy}
              onClick={submitAnswer}
              className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              {t('onboarding.cta.continue')}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={onSkip}
              className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text-muted outline-none transition-colors hover:border-border-strong hover:text-text focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              {t('onboarding.cta.skip')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
