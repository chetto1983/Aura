import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { ElicitationField, ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import { ElicitationCard } from '../ElicitationCard';
import type { ElicitationItem } from '../useThreadElicitations';

// The order internal/elicit sends: required fields in the schema's order, then the rest by name.
const NAME: ElicitationField = {
  name: 'name',
  kind: 'string',
  required: true,
  title: 'Name',
  description: 'Your full name',
};
const PET: ElicitationField = {
  name: 'pet',
  kind: 'enum',
  required: true,
  title: 'Pet',
  enum: ['cat', 'dog'],
  enum_titles: ['Cat', ''],
};
const EMAIL: ElicitationField = {
  name: 'email',
  kind: 'string',
  required: false,
  format: 'email',
  title: 'Email',
};
const TOPPINGS: ElicitationField = {
  name: 'toppings',
  kind: 'enum',
  required: false,
  multi: true,
  enum: ['ham', 'egg'],
};

function question(over: Partial<ElicitationQuestion> = {}): ElicitationQuestion {
  return {
    run_id: 'run-1',
    id: 'q-1',
    server: 'forms',
    tool: 'ask_name',
    message: 'Tell me about <b>you</b>',
    fields: [NAME, PET, EMAIL, TOPPINGS],
    deadline: new Date(Date.now() + 300_000).toISOString(),
    ...over,
  };
}

interface Post {
  readonly url: string;
  readonly body: unknown;
  readonly key: string | null;
}

function stubAnswers(posts: Post[], ...responses: Response[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const key = new Headers(init?.headers).get('Idempotency-Key');
      posts.push({ url, body: JSON.parse(init?.body as string), key });
      const delivered = new Response('{"status":"delivered"}', { status: 202 });
      return Promise.resolve(responses.shift() ?? delivered);
    }),
  );
}

function refusal(errors: Record<string, string>): Response {
  return new Response(JSON.stringify({ errors }), { status: 422 });
}

function renderCard(item: ElicitationItem, isStreaming = true) {
  return render(<ElicitationCard item={item} isStreaming={isStreaming} />);
}

const click = (name: string) => {
  fireEvent.click(screen.getByRole('button', { name }));
};

describe('ElicitationCard', () => {
  it('shows an empty validation response and treats prototype field names as ordinary names', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, refusal({}));
    renderCard({ question: question({ fields: [{ ...NAME, name: 'constructor' }] }) });
    const input = screen.getByRole('textbox');
    expect(input.getAttribute('aria-invalid')).toBeNull();
    fireEvent.change(input, { target: { value: 'Ada' } });
    click('Submit');
    expect(await screen.findByRole('status')).toHaveProperty(
      'textContent',
      'This value is not valid.',
    );
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { constructor: 'Ada' } });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('walks one step per field, reviews every answer, and submits only from Review', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question() });

    expect(screen.getByText('Step 1 of 5')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Name' })).toBeTruthy();
    const name = screen.getByRole('textbox');
    expect(name.getAttribute('aria-describedby')).toBe(
      screen.getByText('Your full name').getAttribute('id'),
    );
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Next' }).disabled).toBe(true);
    fireEvent.change(name, { target: { value: 'Ada' } });
    fireEvent.keyDown(name, { key: 'Enter' });

    expect(screen.getByText('Step 2 of 5')).toBeTruthy();
    expect(screen.getByRole('option', { name: 'dog' })).toBeTruthy();
    fireEvent.click(screen.getByRole('option', { name: 'Cat' }));
    click('Next');

    expect(screen.getByRole('textbox').getAttribute('type')).toBe('email');
    click('Skip');

    expect(screen.getByRole('listbox').getAttribute('aria-multiselectable')).toBe('true');
    fireEvent.click(screen.getByRole('option', { name: 'ham' }));
    fireEvent.click(screen.getByRole('option', { name: 'egg' }));
    click('Back');
    expect(screen.getByRole('heading', { name: 'Email' })).toBeTruthy();
    click('Next');
    expect(screen.queryByRole('button', { name: 'Submit' })).toBeNull();
    click('Review');

    expect(screen.getByText('Step 5 of 5')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Review your answers' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Change Name/ }).textContent).toContain('Ada');
    expect(screen.getByRole('button', { name: /Change Email/ }).textContent).toContain('Not given');
    fireEvent.click(screen.getByRole('button', { name: /Change Pet/ }));
    expect(screen.getByRole('option', { name: 'Cat' }).getAttribute('aria-selected')).toBe('true');
    click('Next');
    click('Next');
    click('Review');
    expect(posts).toHaveLength(0);
    click('Submit');

    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.url).toBe('/agent/runs/run-1/elicitations/q-1');
    expect(posts[0]?.body).toEqual({
      action: 'accept',
      content: { name: 'Ada', pet: 'cat', toppings: ['ham', 'egg'] },
    });
    expect(posts[0]?.key).toMatch(/^[0-9a-f-]{36}$/);
  });

  it('draws a boolean as Yes and No, prefilled, and a number with its bounds', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const age: ElicitationField = {
      name: 'age',
      kind: 'integer',
      required: true,
      min: 0,
      max: 150,
    };
    const agree: ElicitationField = {
      name: 'agree',
      kind: 'boolean',
      required: true,
      default: true,
    };
    renderCard({ question: question({ fields: [age, agree] }) });

    const input = screen.getByRole('spinbutton');
    const bounds = ['min', 'max', 'step'].map((attribute) => input.getAttribute(attribute));
    expect(bounds).toEqual(['0', '150', '1']);
    fireEvent.change(input, { target: { value: '36' } });
    click('Next');
    expect(screen.getByRole('option', { name: 'Yes' }).getAttribute('aria-selected')).toBe('true');
    click('Review');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { age: 36, agree: true } });
  });

  it('bounds a string by its lengths, takes a decimal, and sends nothing for a cleared input', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const code: ElicitationField = {
      name: 'code',
      kind: 'string',
      required: false,
      min_length: 4,
      max_length: 8,
    };
    const ratio: ElicitationField = { name: 'ratio', kind: 'number', required: false };
    const at: ElicitationField = {
      name: 'at',
      kind: 'string',
      required: false,
      format: 'date-time',
    };
    renderCard({ question: question({ fields: [code, ratio, at] }) });

    const text = screen.getByRole('textbox');
    expect(['minlength', 'maxlength'].map((attribute) => text.getAttribute(attribute))).toEqual([
      '4',
      '8',
    ]);
    fireEvent.change(text, { target: { value: 'abcd' } });
    fireEvent.change(text, { target: { value: '' } });
    fireEvent.keyDown(text, { key: 'Tab' });
    expect(screen.getByText('Step 1 of 4')).toBeTruthy();
    click('Next');

    const decimal = screen.getByRole('spinbutton');
    expect(decimal.getAttribute('step')).toBe('any');
    fireEvent.change(decimal, { target: { value: '0.5' } });
    click('Next');

    // A seconds-precise default must fit, so the picker steps by the second.
    expect(document.querySelector('input[type="datetime-local"]')?.getAttribute('step')).toBe('1');
    click('Review');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { ratio: 0.5 } });
  });

  // Review Focus 5.
  it('a 422 puts the card back on the failing step, with the error there', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, refusal({ email: 'format' }));
    renderCard({ question: question({ fields: [NAME, EMAIL] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Next');
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'nope' } });
    click('Review');
    click('Submit');

    await waitFor(() => {
      expect(screen.getByText('Step 2 of 3')).toBeTruthy();
    });
    const email = screen.getByRole<HTMLInputElement>('textbox');
    expect(email.value).toBe('nope');
    expect(email.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByText('This value is not valid.')).toBeTruthy();
    fireEvent.change(email, { target: { value: 'ada@example.com' } });
    expect(email.getAttribute('aria-invalid')).toBeNull();
    click('Review');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(2);
    });
    expect(posts[1]?.body).toEqual({
      action: 'accept',
      content: { email: 'ada@example.com', name: 'Ada' },
    });
  });

  it('reads a required-field refusal as required, and a whole-answer one on the card', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, refusal({ name: 'required', '': 'not_asked' }));
    renderCard({ question: question({ fields: [NAME] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Submit');
    expect(await screen.findByText('This field is required.')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe('This value is not valid.');
  });

  it('says Use default on a defaulted field, and the receipt shows what the server receives', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const color: ElicitationField = {
      name: 'color',
      kind: 'string',
      required: false,
      default: 'blue',
    };
    const q = question({ fields: [NAME, color] });
    const { rerender } = renderCard({ question: q });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Next');
    expect(screen.getByRole<HTMLInputElement>('textbox').value).toBe('blue');
    expect(screen.queryByRole('button', { name: 'Skip' })).toBeNull();
    click('Use default');
    expect(screen.getByRole('button', { name: /Change color/ }).textContent).toContain('blue');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { name: 'Ada' } });
    rerender(<ElicitationCard item={{ question: q, outcome: 'accepted' }} isStreaming />);
    expect(screen.getByText('color')).toBeTruthy();
    expect(screen.getByText('blue')).toBeTruthy();
  });

  it('says how many a bounded multi choice takes, and holds Submit outside the bounds', () => {
    const tags: ElicitationField = {
      name: 'tags',
      kind: 'enum',
      required: false,
      multi: true,
      enum: ['a', 'b', 'c'],
      min_items: 1,
      max_items: 2,
    };
    renderCard({ question: question({ fields: [tags] }) });
    const hint = screen.getByText('Choose 1 to 2.');
    expect(screen.getByRole('listbox').getAttribute('aria-describedby')).toBe(hint.id);
    const submit = screen.getByRole<HTMLButtonElement>('button', { name: 'Submit' });
    expect(submit.disabled).toBe(false);
    for (const option of ['a', 'b', 'c'])
      fireEvent.click(screen.getByRole('option', { name: option }));
    expect(submit.disabled).toBe(true);
    fireEvent.click(screen.getByRole('option', { name: 'c' }));
    expect(submit.disabled).toBe(false);
  });

  it('shows the receipt, with the answer given, once the stream says it was accepted', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const q = question({ fields: [NAME] });
    const { rerender } = renderCard({ question: q });
    expect(screen.queryByText(/Step \d/)).toBeNull();
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    rerender(<ElicitationCard item={{ question: q, outcome: 'accepted' }} isStreaming />);
    const chip = screen.getByText('Answered.').closest('[data-tone]');
    expect(chip?.getAttribute('data-tone')).toBe('success');
    expect(screen.getByText('Ada')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('makes every other ending a muted receipt', () => {
    for (const [outcome, label] of [
      ['declined', 'Declined.'],
      ['cancelled', 'Cancelled.'],
      ['expired', 'Expired: cancelled automatically.'],
    ] as const) {
      const { unmount } = renderCard({ question: question(), outcome });
      const chip = screen.getByText(label).closest('[data-tone]');
      expect(chip?.getAttribute('data-tone')).toBe('neutral');
      unmount();
    }
  });

  it('always offers Decline, and Cancel asks first while the run streams', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question() });
    click('Cancel');
    expect(screen.getByText('Cancel this request?')).toBeTruthy();
    expect(posts).toHaveLength(0);
    click('Cancel request');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'cancel' });
  });

  it('declines without sending anything the operator typed', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question({ fields: [NAME] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'secret' } });
    click('Decline');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'decline' });
  });

  it('says a closed form was already resolved, and a failed send can be retried', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, new Response('{"error":"question already resolved"}', { status: 409 }));
    const { unmount } = renderCard({ question: question() });
    click('Decline');
    expect(await screen.findByText('This form was already resolved.')).toBeTruthy();
    unmount();

    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('offline'))),
    );
    renderCard({ question: question() });
    click('Decline');
    expect(await screen.findByText("Couldn't send your answer. Try again.")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Decline' }).disabled).toBe(false);
  });

  it('a form Aura refused says why and offers nothing', () => {
    renderCard({ question: question({ refusal: 'ambiguous_run', fields: [], message: '' }) });
    expect(
      screen.getByText(
        'Aura declined this form because more than one conversation was using this server.',
      ),
    ).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.queryByRole('timer')).toBeNull();
  });

  it("names the server by its mount, keeps the message as plain text, and counts down Aura's bound", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-25T10:00:00Z'));
    renderCard({ question: question({ deadline: '2026-09-25T10:05:00Z' }) });
    expect(screen.getByText('forms')).toBeTruthy();
    expect(screen.getByText('ask_name')).toBeTruthy();
    const message = screen.getByText('Tell me about <b>you</b>');
    expect(screen.getByRole('form').getAttribute('aria-describedby')).toBe(message.id);
    expect(document.querySelector('b')).toBeNull();
    const timer = screen.getByRole('timer');
    expect(timer.getAttribute('aria-label')).toBe('Aura cancels in 5:00');
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByRole('timer').textContent).toContain('4:59');
  });
});
