import { useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronLeft } from 'lucide-react';
import type { ElicitationAction } from '../chat/sseAdapter_elicitation';
import { CancelControl } from './CancelControl';
import { ElicitationHeader } from './ElicitationHeader';
import { ElicitationReview } from './ElicitationReview';
import { FieldInput } from './FieldInput';
import { QuestionCard, type QuestionCardProps } from './QuestionCard';
import { QuestionOptions } from './QuestionOptions';
import { QuestionReceipt, type ReceiptTone } from './QuestionReceipt';
import { postElicitationAnswer, PROBLEM_REQUIRED } from './elicitationApi';
import {
  contentFrom,
  fieldTitle,
  firstFailingStep,
  hasValue,
  initialValues,
  itemsHint,
  optionsFor,
  selectedIds,
  summaryOf,
  toggleValue,
  withinItemBounds,
  type FieldValue,
  type FieldValues,
} from './elicitationSteps';
import type { ElicitationItem, ElicitationOutcome } from './useThreadElicitations';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

// ElicitationCard is a mounted MCP server's form drawn as Question Flow (spec 2026-09-25): one
// step per field with Back and Next, then a Review step that alone submits, and Decline and
// Cancel always in the footer. The server's message is the description on every step. The
// answer goes to the run's route; how the question closed arrives on the stream, so the
// receipt shows the server's state rather than the card's guess.

interface Receipt {
  readonly tone: ReceiptTone;
  readonly key: string;
}

const RECEIPTS: Record<ElicitationOutcome, Receipt> = {
  accepted: { tone: 'success', key: 'questionCard.receipt.answered' },
  declined: { tone: 'neutral', key: 'questionCard.receipt.declined' },
  cancelled: { tone: 'neutral', key: 'questionCard.receipt.cancelled' },
  expired: { tone: 'neutral', key: 'questionCard.receipt.expired' },
};

type Problem = 'failed' | 'closed' | 'invalid';

const PROBLEM_KEYS: Record<Problem, string> = {
  failed: 'questionCard.error.failed',
  closed: 'questionCard.error.closed',
  invalid: 'questionCard.error.invalid',
};

export interface ElicitationCardProps {
  readonly item: ElicitationItem;
  readonly isStreaming?: boolean;
}

export function ElicitationCard({ item, isStreaming }: ElicitationCardProps) {
  const { t } = useTranslation();
  const baseId = useId();
  const { question, outcome } = item;
  const { fields } = question;
  const [step, setStep] = useState(0);
  const [values, setValues] = useState<FieldValues>(() => initialValues(fields));
  const [errors, setErrors] = useState<Readonly<Record<string, string>>>({});
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<ElicitationAction | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const titleId = `${baseId}-title`;
  const labels = { yes: t('questionCard.yes'), no: t('questionCard.no') };
  const settled = outcome !== undefined || question.refusal !== undefined;

  function frame(children: ReactNode, extra: Partial<QuestionCardProps> = {}) {
    return (
      <QuestionCard
        titleId={titleId}
        title={t('questionCard.form.title', { server: question.server })}
        header={<ElicitationHeader question={question} countdown={!settled} />}
        {...(question.message !== ''
          ? { description: question.message, descriptionId: `${baseId}-message` }
          : {})}
        dataAttributes={{ 'data-elicitation-id': question.id }}
        {...extra}
      >
        {children}
      </QuestionCard>
    );
  }

  if (question.refusal !== undefined) {
    const label = t(`questionCard.refusal.${question.refusal}`);
    return frame(<QuestionReceipt tone="neutral" label={label} />);
  }
  if (outcome !== undefined) {
    const receipt = RECEIPTS[outcome];
    const summary =
      outcome === 'accepted' && sent === 'accept' ? summaryOf(fields, values, labels) : [];
    return frame(
      <QuestionReceipt tone={receipt.tone} label={t(receipt.key)} announce summary={summary} />,
    );
  }

  // A form of more than one field ends on Review, and only Review submits.
  const reviewStep = fields.length > 1 ? fields.length : null;
  const reviewing = step === reviewStep;
  const submits = reviewing || reviewStep === null;
  const field = reviewing ? undefined : fields[step];
  const locked = busy || sent !== null;
  const value = field === undefined ? undefined : values[field.name];
  const error =
    field !== undefined && Object.hasOwn(errors, field.name) ? errors[field.name] : undefined;
  const canGoOn =
    field === undefined || ((!field.required || hasValue(value)) && withinItemBounds(field, value));
  const hint = field === undefined ? null : itemsHint(field);
  const ids = {
    hint: `${baseId}-hint`,
    items: `${baseId}-items`,
    error: `${baseId}-error`,
  };
  const describedBy = [
    field?.description !== undefined ? ids.hint : '',
    hint !== null ? ids.items : '',
    error !== undefined ? ids.error : '',
  ]
    .filter((id) => id !== '')
    .join(' ');
  const described = describedBy === '' ? {} : { describedBy };

  async function send(action: ElicitationAction, current: FieldValues = values) {
    setBusy(true);
    setProblem(null);
    try {
      const result = await postElicitationAnswer(
        question.run_id,
        question.id,
        action === 'accept' ? { action, content: contentFrom(fields, current) } : { action },
        crypto.randomUUID(),
      );
      if (result.kind === 'delivered') {
        setSent(action);
      } else if (result.kind === 'invalid') {
        setErrors(result.errors);
        setStep(firstFailingStep(fields, result.errors));
        if (
          result.errors[''] !== undefined ||
          !fields.some((entry) => Object.hasOwn(result.errors, entry.name))
        ) {
          setProblem('invalid');
        }
      } else {
        setProblem('closed');
      }
    } catch {
      setProblem('failed');
    } finally {
      setBusy(false);
    }
  }

  function advance(current: FieldValues = values) {
    if (submits) void send('accept', current);
    else setStep(step + 1);
  }

  function update(name: string, next: FieldValue | undefined) {
    setValues((current) => ({ ...current, [name]: next }));
    setErrors((current) =>
      Object.fromEntries(Object.entries(current).filter(([key]) => key !== name)),
    );
  }

  // Skip leaves the field out. On a field with a default the button says Use default: go-sdk
  // puts the default back on every accept (mcp/client.go:901), so leaving it out sends it.
  function skip() {
    if (field === undefined) return;
    const cleared = { ...values, [field.name]: undefined };
    setValues(cleared);
    advance(cleared);
  }

  const next = () => {
    if (canGoOn && !locked) advance();
  };

  let body: ReactNode = null;
  if (reviewing) {
    body = (
      <ElicitationReview
        fields={fields}
        values={values}
        labels={labels}
        disabled={locked}
        onEdit={setStep}
      />
    );
  } else if (field?.kind === 'enum' || field?.kind === 'boolean') {
    body = (
      <QuestionOptions
        key={field.name}
        labelledBy={titleId}
        {...described}
        options={optionsFor(field, labels)}
        mode={field.kind === 'enum' && field.multi === true ? 'multi' : 'single'}
        selected={selectedIds(value)}
        disabled={locked}
        onToggle={(id) => {
          update(field.name, toggleValue(field, value, id));
        }}
        onSubmit={next}
      />
    );
  } else if (field !== undefined) {
    body = (
      <FieldInput
        key={field.name}
        field={field}
        labelledBy={titleId}
        {...described}
        value={value}
        invalid={error !== undefined}
        disabled={locked}
        onChange={(changed) => {
          update(field.name, changed);
        }}
        onEnter={next}
      />
    );
  }

  let title: string | undefined;
  if (reviewing) title = t('questionCard.reviewStep.title');
  else if (field !== undefined) title = fieldTitle(field);
  const primary = submits ? 'submit' : step === fields.length - 1 ? 'review' : 'next';
  const footer = (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={locked}
          onClick={() => void send('decline')}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('questionCard.decline')}
        </Button>
        <CancelControl
          isStreaming={isStreaming}
          disabled={locked}
          labels={{
            cancel: t('questionCard.cancel.label'),
            confirm: t('questionCard.cancel.confirm'),
            yes: t('questionCard.cancel.yes'),
            no: t('questionCard.cancel.no'),
          }}
          onCancel={() => void send('cancel')}
        />
      </div>
      <div className="flex items-center gap-2">
        {step > 0 ? (
          <Button
            type="button"
            variant="ghost"
            disabled={locked}
            onClick={() => {
              setStep(step - 1);
            }}
            className="gap-1 rounded-full text-text-muted"
          >
            <ChevronLeft aria-hidden="true" className="size-4" />
            {t('questionCard.back')}
          </Button>
        ) : null}
        {field !== undefined && !field.required ? (
          <Button
            type="button"
            variant="ghost"
            disabled={locked}
            onClick={skip}
            className="rounded-full text-text-muted"
          >
            {t(field.default !== undefined ? 'questionCard.useDefault' : 'questionCard.skip')}
          </Button>
        ) : null}
        <Button type="button" disabled={locked || !canGoOn} onClick={next} className="rounded-full">
          {t(`questionCard.${primary}`)}
        </Button>
      </div>
    </>
  );

  const status =
    problem === null ? null : (
      <Alert
        role="status"
        aria-live="polite"
        data-tone="danger"
        variant="destructive"
        className="bg-surface"
      >
        <AlertDescription>{t(PROBLEM_KEYS[problem])}</AlertDescription>
      </Alert>
    );

  return frame(
    <>
      {body}
      {field?.description !== undefined ? (
        <p id={ids.hint} className="text-xs text-text-muted">
          {field.description}
        </p>
      ) : null}
      {hint !== null ? (
        <p id={ids.items} className="text-xs text-text-muted">
          {t(`questionCard.choose.${hint.key}`, hint.params)}
        </p>
      ) : null}
      {error !== undefined ? (
        <p id={ids.error} className="text-[0.8125rem] text-danger">
          {t(
            error === PROBLEM_REQUIRED
              ? 'questionCard.error.required'
              : 'questionCard.error.invalid',
          )}
        </p>
      ) : null}
    </>,
    {
      ...(title !== undefined ? { title } : {}),
      step: {
        current: step + 1,
        total: Math.max(fields.length + (reviewStep === null ? 0 : 1), 1),
      },
      footer,
      ...(status !== null ? { status } : {}),
    },
  );
}
