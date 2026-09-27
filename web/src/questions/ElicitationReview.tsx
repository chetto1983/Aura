import { useTranslation } from 'react-i18next';
import type { ElicitationField } from '../chat/sseAdapter_elicitation';
import { fieldTitle, receivedText, type BooleanLabels, type FieldValues } from './elicitationSteps';

// ElicitationReview is an MCP form's last step: every field with what the server will receive,
// each row leading back to its step. The MCP spec has clients let the operator review and
// change answers before sending (2025-11-25 elicitation.mdx:40-45).

export interface ElicitationReviewProps {
  readonly fields: readonly ElicitationField[];
  readonly values: FieldValues;
  readonly labels: BooleanLabels;
  readonly disabled: boolean;
  readonly onEdit: (step: number) => void;
}

export function ElicitationReview({
  fields,
  values,
  labels,
  disabled,
  onEdit,
}: ElicitationReviewProps) {
  const { t } = useTranslation();
  return (
    <ul className="flex flex-col divide-y divide-border-strong/40">
      {fields.map((field, index) => {
        const title = fieldTitle(field);
        const shown = receivedText(field, values[field.name], labels);
        return (
          <li key={field.name}>
            <button
              type="button"
              disabled={disabled}
              onClick={() => {
                onEdit(index);
              }}
              className="flex w-full flex-col gap-0.5 rounded-lg px-1 py-2 text-left text-sm outline-none hover:bg-accent/5 focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              <span className="sr-only">{t('questionCard.reviewStep.edit', { field: title })}</span>
              <span aria-hidden="true" className="text-text-muted">
                {title}
              </span>
              <span className="font-medium break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
                {shown ?? t('questionCard.reviewStep.notGiven')}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
