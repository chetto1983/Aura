import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { OnboardingAnswers, OnboardingStepResponse } from './onboardingApi';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

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
  readonly skipLabel?: string;
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
  skipLabel,
}: InterviewStepProps) {
  const { t } = useTranslation();
  const answerId = useId();
  const [answer, setAnswer] = useState('');
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [editRole, setEditRole] = useState('');

  const draftMode = isDraft(step);
  const effectiveSkipLabel = skipLabel ?? t('onboarding.cta.skip');

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
            <Card className="gap-3 bg-surface p-4">
              <div className="flex flex-col gap-1">
                <Label htmlFor={`${answerId}-name`} className="text-[13px] font-semibold text-text">
                  {t('onboarding.interview.editNameLabel')}
                </Label>
                <Input
                  id={`${answerId}-name`}
                  type="text"
                  value={editName}
                  onChange={(e) => {
                    setEditName(e.target.value);
                  }}
                  className="bg-surface-2 text-[15.5px]"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor={`${answerId}-role`} className="text-[13px] font-semibold text-text">
                  {t('onboarding.interview.editRoleLabel')}
                </Label>
                <Input
                  id={`${answerId}-role`}
                  type="text"
                  value={editRole}
                  onChange={(e) => {
                    setEditRole(e.target.value);
                  }}
                  className="bg-surface-2 text-[15.5px]"
                />
              </div>
              <Button type="button" disabled={busy} onClick={submitEdit}>
                {t('onboarding.cta.applyEdit')}
              </Button>
            </Card>
          ) : null}

          <div className="flex flex-wrap gap-2">
            <Button type="button" disabled={busy} onClick={onConfirm}>
              {t('onboarding.cta.confirm')}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => {
                setEditing((v) => !v);
              }}
            >
              {t('onboarding.cta.edit')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={busy}
              onClick={onSkip}
              className="border border-border bg-surface-2 text-text-muted hover:border-border-strong hover:text-text"
            >
              {effectiveSkipLabel}
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor={answerId} className="text-[13px] font-semibold text-text">
              {t('onboarding.interview.answerLabel')}
            </Label>
            <Textarea
              id={answerId}
              rows={4}
              value={answer}
              placeholder={t('onboarding.interview.answerPlaceholder')}
              onChange={(e) => {
                setAnswer(e.target.value);
              }}
              className="bg-surface-2 text-[15.5px] leading-relaxed"
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="button" disabled={busy} onClick={submitAnswer}>
              {t('onboarding.cta.continue')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={busy}
              onClick={onSkip}
              className="border border-border bg-surface-2 text-text-muted hover:border-border-strong hover:text-text"
            >
              {effectiveSkipLabel}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
