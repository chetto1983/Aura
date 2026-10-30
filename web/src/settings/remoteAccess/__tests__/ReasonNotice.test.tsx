import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import i18n from '../../../i18n/i18n';
import { remoteAccessEn, remoteAccessIt } from '../../../i18n/resources.remoteAccess';
import { ReasonNotice, StepFailure } from '../ReasonNotice';
import { REMOTE_ACCESS_REASONS, type RemoteAccessStatusDTO } from '../remoteAccessApi';
import { STEP_KEYS } from '../remoteAccessSteps';

const handlers = () => ({ onOpenStep: vi.fn(), onFix: vi.fn(), onRetry: vi.fn() });

function strings(node: unknown): string[] {
  if (typeof node === 'string') return [node];
  if (node === null || typeof node !== 'object') return [];
  return Object.values(node).flatMap(strings);
}

afterEach(async () => {
  await i18n.changeLanguage('en');
});

describe('every reason says what went wrong and what to do', () => {
  it.each(REMOTE_ACCESS_REASONS)('%s has its sentence and action in both locales', (reason) => {
    for (const reasons of [
      remoteAccessEn.remoteAccess.reasons,
      remoteAccessIt.remoteAccess.reasons,
    ]) {
      expect(reasons[reason].sentence.length).toBeGreaterThan(20);
      expect(reasons[reason].action.length).toBeGreaterThan(3);
    }
  });

  it('no two reasons share a sentence: each could only have been written knowing the error', () => {
    for (const { reasons } of [remoteAccessEn.remoteAccess, remoteAccessIt.remoteAccess]) {
      const sentences = REMOTE_ACCESS_REASONS.map((reason) => reasons[reason].sentence);
      expect(new Set(sentences).size).toBe(sentences.length);
    }
  });
});

describe('the operator reads plain words', () => {
  const banned = [
    /tunnel con nome/i,
    /named tunnel/i,
    /\bSSE\b/,
    /riconciliazion/i,
    /reconciliation/i,
    /proiettato/i,
    /projected/i,
    /membership/i,
    /\bWARP\b/,
    /Gateway/,
  ];

  it.each([
    ['en', remoteAccessEn],
    ['it', remoteAccessIt],
  ])('%s carries no banned word', (_locale, resource) => {
    const copy = strings(resource);
    expect(copy.length).toBeGreaterThan(40);
    for (const line of copy) {
      for (const word of banned) expect(line).not.toMatch(word);
    }
  });

  it('the Italian keeps its accents', () => {
    for (const line of strings(remoteAccessIt)) {
      expect(line).not.toMatch(/\b(puo|finche|perche|gia|piu|e disponibile|e stato)\b/i);
    }
  });

  it('both locales carry the same keys', () => {
    const keys = (node: object, prefix = ''): string[] =>
      Object.entries(node).flatMap(([key, value]: [string, unknown]) =>
        value !== null && typeof value === 'object'
          ? keys(value, `${prefix}${key}.`)
          : [`${prefix}${key}`],
      );
    expect(keys(remoteAccessIt)).toEqual(keys(remoteAccessEn));
  });
});

describe('ReasonNotice', () => {
  it('is an alert carrying the sentence and its action', async () => {
    await i18n.changeLanguage('it');
    render(<ReasonNotice reason="domain_elsewhere" step="domain" {...handlers()} />);
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain(
      remoteAccessIt.remoteAccess.reasons.domain_elsewhere.sentence,
    );
    expect(alert.querySelector('svg')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Usa un altro dominio' })).toBeTruthy();
  });

  it('on its own step a domain or token reason hands the field back', () => {
    const act = handlers();
    render(<ReasonNotice reason="token_rejected" step="cloudflare" {...act} />);
    fireEvent.click(screen.getByRole('button'));
    expect(act.onFix).toHaveBeenCalledOnce();
    expect(act.onOpenStep).not.toHaveBeenCalled();
    expect(act.onRetry).not.toHaveBeenCalled();
  });

  it('elsewhere it opens the step that can fix it', () => {
    const act = handlers();
    render(<ReasonNotice reason="token_rejected" step="domain" {...act} />);
    fireEvent.click(screen.getByRole('button'));
    expect(act.onOpenStep).toHaveBeenCalledExactlyOnceWith('cloudflare');
    expect(act.onFix).not.toHaveBeenCalled();
  });

  it('a connection reason retries', () => {
    const act = handlers();
    render(<ReasonNotice reason="cloudflare_unreachable" step="connection" {...act} />);
    fireEvent.click(screen.getByRole('button'));
    expect(act.onRetry).toHaveBeenCalledOnce();
    expect(act.onOpenStep).not.toHaveBeenCalled();
  });

  it('holds its button while the retry runs', () => {
    render(<ReasonNotice reason="unknown" step="connection" retrying {...handlers()} />);
    expect(screen.getByRole('button').hasAttribute('disabled')).toBe(true);
  });

  it.each(REMOTE_ACCESS_REASONS)('%s has a button on every step', (reason) => {
    for (const step of STEP_KEYS) {
      const { unmount } = render(<ReasonNotice reason={reason} step={step} {...handlers()} />);
      expect(screen.getAllByRole('button')).toHaveLength(1);
      unmount();
    }
  });
});

describe('StepFailure', () => {
  const status: RemoteAccessStatusDTO = {
    enabled: true,
    phase: 'error',
    api_token_set: true,
    generation: 3,
    nameservers: [],
    reason: 'hostname_taken',
    accepted: false,
    reachable: false,
    abandoned: [],
  };

  it('a press that failed with a reason replaces what the row says', async () => {
    await i18n.changeLanguage('it');
    render(<StepFailure status={status} step="domain" failure="domain_invalid" {...handlers()} />);
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'Correggi il dominio' })).toBeTruthy();
  });

  // A failed retry must not take away the button that retries: that would be a dead end.
  it('a press that failed with no reason keeps the row’s own way forward', async () => {
    await i18n.changeLanguage('it');
    render(<StepFailure status={status} step="domain" failure="stale" {...handlers()} />);
    const [line, notice] = screen.getAllByRole('alert');
    expect(line?.textContent).toContain('cambiata nel frattempo');
    expect(notice?.querySelector('button')?.textContent).toBe('Scegli un’altra etichetta');
  });

  it('with no failed press it shows the row’s reason on its own step', async () => {
    await i18n.changeLanguage('it');
    render(<StepFailure status={status} step="domain" failure={null} {...handlers()} />);
    expect(screen.getByRole('button', { name: 'Scegli un’altra etichetta' })).toBeTruthy();
  });

  it('shows nothing on another step', () => {
    render(<StepFailure status={status} step="connection" failure={null} {...handlers()} />);
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('says plainly when the server named nothing', async () => {
    await i18n.changeLanguage('it');
    render(
      <StepFailure
        status={{ ...status, phase: 'disabled', reason: '' }}
        step="domain"
        failure="generic"
        {...handlers()}
      />,
    );
    expect(screen.getByRole('alert').textContent).toContain('Riprova');
  });
});
