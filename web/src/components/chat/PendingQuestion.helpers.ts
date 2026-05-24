export type PendingQuestionKind = 'approval' | 'selection' | 'clarification' | 'secret_input' | string;

export type PendingQuestionOption = {
  label: string;
  value: string;
};

export type PendingQuestionPayload = {
  id: string;
  question: string;
  options: PendingQuestionOption[];
  kind: PendingQuestionKind;
};

export type PendingQuestionAnswerRequest = {
  thread_id?: string;
  answer: string;
  selected_option_ids?: string[];
};

type MaybePendingQuestionPart = {
  type?: unknown;
  name?: unknown;
  data?: unknown;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === 'string' ? value.trim() : '';
}

export function normalizePendingQuestionPayload(value: unknown): PendingQuestionPayload | null {
  const payload = pendingQuestionData(value);
  if (!payload) return null;

  const id = stringField(payload, 'id');
  const question = stringField(payload, 'question');
  if (!id || !question) return null;

  return {
    id,
    question,
    options: normalizeOptions(payload.options),
    kind: stringField(payload, 'kind') || 'clarification',
  };
}

export function isPendingQuestionPart(part: unknown): boolean {
  if (!isRecord(part)) return false;
  const maybe = part as MaybePendingQuestionPart;
  return maybe.type === 'data' && (maybe.name === 'pending-question' || maybe.name === 'pending_question');
}

export function firstPendingQuestionPayload(values: readonly unknown[] | undefined): PendingQuestionPayload | null {
  for (const value of values ?? []) {
    const payload = normalizePendingQuestionPayload(value);
    if (payload) return payload;
  }
  return null;
}

export function requiresTextInput(kind: PendingQuestionKind, optionCount: number): boolean {
  return kind === 'clarification' || kind === 'secret_input' || optionCount === 0;
}

export function buildAnswerRequest(params: {
  answer: string;
  threadID?: string;
  selectedOptionID?: string;
}): PendingQuestionAnswerRequest {
  const answer = params.answer.trim();
  const req: PendingQuestionAnswerRequest = { answer };
  if (params.threadID?.trim()) req.thread_id = params.threadID.trim();
  if (params.selectedOptionID?.trim()) req.selected_option_ids = [params.selectedOptionID.trim()];
  return req;
}

function pendingQuestionData(value: unknown): Record<string, unknown> | null {
  if (!isRecord(value)) return null;
  if (isPendingQuestionPart(value)) {
    return isRecord(value.data) ? value.data : null;
  }
  if (
    (value.name === 'pending-question' || value.name === 'pending_question') &&
    isRecord(value.data)
  ) {
    return value.data;
  }
  return value;
}

function normalizeOptions(raw: unknown): PendingQuestionOption[] {
  if (!Array.isArray(raw)) return [];
  return raw
    .map((item) => {
      if (typeof item === 'string') {
        const value = item.trim();
        return value ? { label: value, value } : null;
      }
      if (!isRecord(item)) return null;
      const value = stringField(item, 'value') || stringField(item, 'id') || stringField(item, 'label');
      const label = stringField(item, 'label') || value;
      return value ? { label, value } : null;
    })
    .filter((item): item is PendingQuestionOption => item !== null);
}
