import { describe, expect, it } from 'vitest';
import {
  buildAnswerRequest,
  firstPendingQuestionPayload,
  isPendingQuestionPart,
  normalizePendingQuestionPayload,
  requiresTextInput,
} from './PendingQuestion.helpers';

describe('pending question helpers', () => {
  it('normalizes assistant-ui data-pending-question parts', () => {
    const payload = normalizePendingQuestionPayload({
      type: 'data',
      name: 'pending-question',
      data: {
        id: 'question-1',
        question: 'Continue?',
        options: ['approve', 'deny'],
        kind: 'approval',
      },
    });

    expect(payload).toEqual({
      id: 'question-1',
      question: 'Continue?',
      options: [
        { label: 'approve', value: 'approve' },
        { label: 'deny', value: 'deny' },
      ],
      kind: 'approval',
    });
    expect(isPendingQuestionPart({ type: 'data', name: 'pending-question' })).toBe(true);
  });

  it('finds pending questions stored as assistant-ui metadata data entries', () => {
    const payload = firstPendingQuestionPayload([
      { name: 'other', data: { id: 'ignored', question: 'ignored' } },
      {
        name: 'pending-question',
        data: {
          id: 'question-meta',
          question: 'Need input?',
          kind: 'clarification',
        },
      },
    ]);

    expect(payload?.id).toBe('question-meta');
    expect(payload?.question).toBe('Need input?');
  });

  it('supports future label/value option objects without changing the card', () => {
    const payload = normalizePendingQuestionPayload({
      id: 'question-2',
      question: 'Pick one',
      options: [{ label: 'Alpha path', value: 'alpha' }],
      kind: 'selection',
    });

    expect(payload?.options).toEqual([{ label: 'Alpha path', value: 'alpha' }]);
  });

  it('keeps free-text inputs for clarification, secret input, and missing options', () => {
    expect(requiresTextInput('clarification', 2)).toBe(true);
    expect(requiresTextInput('secret_input', 2)).toBe(true);
    expect(requiresTextInput('selection', 0)).toBe(true);
    expect(requiresTextInput('selection', 2)).toBe(false);
  });

  it('builds the answer body with thread scope and selected option id', () => {
    expect(buildAnswerRequest({
      answer: ' approve ',
      threadID: ' web_thread ',
      selectedOptionID: ' approve ',
    })).toEqual({
      answer: 'approve',
      thread_id: 'web_thread',
      selected_option_ids: ['approve'],
    });
  });
});
