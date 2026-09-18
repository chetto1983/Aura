import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { StudioHistory } from '../StudioHistory';
import { StudioStage } from '../StudioStage';
import type { StudioRecord } from '../studioApi';

// The history panel and the centre it drives. What is asserted is what the operator can act
// on: a card per generation with the status it actually has, a search that narrows to the
// prompt typed, a selection that reaches the caller, a "Load more" that appears only when
// there IS more, and a collapse that survives a remount.

// The base carries no asset and no cost: those arrive when a generation finishes, and a row
// that never got there simply has neither. Adding them by override keeps every fixture free
// of an explicit `undefined`, which exactOptionalPropertyTypes rejects anyway.
function record(over: Partial<StudioRecord> = {}): StudioRecord {
  return {
    id: 'job-1',
    kind: 'video',
    status: 'completed',
    model: 'google/veo-3.1-lite',
    prompt: 'a harbour at dawn',
    used: { aspect_ratio: '16:9', duration: 4, resolution: '720p' },
    created_at: '2026-09-17T10:00:00Z',
    ...over,
  };
}

const DONE = record({ cost_usd: 0.1188, asset_id: 'asset-1' });
const RUNNING = record({ id: 'job-2', status: 'in_progress', prompt: 'a storm' });
const FAILED = record({
  id: 'job-3',
  status: 'failed',
  prompt: 'a refusal',
  error: { code: 'no_key', message: 'openrouter credential missing' },
});

interface PanelOptions {
  readonly records?: readonly StudioRecord[];
  readonly hasMore?: boolean;
  readonly selectedId?: string;
}

function mountPanel(options: PanelOptions = {}) {
  const onSelect = vi.fn();
  const onLoadMore = vi.fn();
  const view = render(
    <StudioHistory
      records={options.records ?? [DONE]}
      selectedId={options.selectedId}
      hasMore={options.hasMore ?? false}
      loadingMore={false}
      onSelect={onSelect}
      onLoadMore={onLoadMore}
    />,
  );
  return { onSelect, onLoadMore, view };
}

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('StudioHistory', () => {
  it('renders one card per generation, each with the status it actually has', () => {
    mountPanel({ records: [RUNNING, FAILED, DONE] });
    expect(screen.getAllByRole('listitem')).toHaveLength(3);
    expect(screen.getByRole('button', { name: /a storm/ }).textContent).toContain('Generating');
    expect(screen.getByRole('button', { name: /a refusal/ }).textContent).toContain('Failed');
    expect(screen.getByRole('button', { name: /a harbour at dawn/ }).textContent).toContain(
      'Ready',
    );
  });

  it('draws a clip from its own first frame and never downloads the whole file for it', () => {
    const { view } = mountPanel();
    const still = view.container.querySelector('video');
    expect(still?.getAttribute('preload')).toBe('metadata');
    expect(still?.getAttribute('src')).toBe('/api/assets/asset-1/stream');
    expect(still?.hasAttribute('controls')).toBe(false);
  });

  it('filters by prompt and says so when nothing matches', () => {
    mountPanel({ records: [DONE, RUNNING] });
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search the history' }), {
      target: { value: 'storm' },
    });
    expect(screen.getByRole('button', { name: /a storm/ })).toBeTruthy();
    // The non-matching card is gone, not merely dimmed.
    expect(screen.queryByRole('button', { name: /a harbour at dawn/ })).toBeNull();

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search the history' }), {
      target: { value: 'nothing like this' },
    });
    expect(screen.getByText('No generation matches that.')).toBeTruthy();
    // "nothing yet" would be false: there ARE generations, none of them match.
    expect(screen.queryByText('Nothing generated yet.')).toBeNull();
  });

  it('names the statuses it knows and shows a later one verbatim rather than blank', () => {
    mountPanel({
      records: [
        record({ id: 'job-x', status: 'expired', prompt: 'an expired one' }),
        record({ id: 'job-y', status: 'cancelled', prompt: 'a cancelled one' }),
        // A status the server mints after this build: showing it raw is the only honest
        // option, and it is better than an empty label.
        record({ id: 'job-z', status: 'quarantined', prompt: 'a future one' }),
      ],
    });
    expect(screen.getByRole('button', { name: /an expired one/ }).textContent).toContain('Expired');
    expect(screen.getByRole('button', { name: /a cancelled one/ }).textContent).toContain(
      'Cancelled',
    );
    expect(screen.getByRole('button', { name: /a future one/ }).textContent).toContain(
      'quarantined',
    );
  });

  it('says nothing has been generated when the history is empty', () => {
    mountPanel({ records: [] });
    expect(screen.getByText('Nothing generated yet.')).toBeTruthy();
  });

  it('reports the card that was clicked, and marks the selected one', () => {
    const { onSelect } = mountPanel({ records: [DONE, RUNNING], selectedId: 'job-2' });
    expect(screen.getByRole('button', { name: /a storm/ }).getAttribute('aria-current')).toBe(
      'true',
    );
    expect(
      screen.getByRole('button', { name: /a harbour at dawn/ }).getAttribute('aria-current'),
    ).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /a harbour at dawn/ }));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect.mock.calls[0]?.[0]).toMatchObject({ id: 'job-1' });
  });

  it('offers Load more only when there is a next page', () => {
    const { onLoadMore, view } = mountPanel();
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
    view.unmount();

    const more = mountPanel({ hasMore: true });
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    expect(more.onLoadMore).toHaveBeenCalledTimes(1);
    expect(onLoadMore).not.toHaveBeenCalled();
  });

  it('collapses on the toggle and is still collapsed after a remount', () => {
    const { view } = mountPanel();
    expect(screen.getByRole('searchbox', { name: 'Search the history' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Hide the history' }));
    expect(screen.queryByRole('searchbox')).toBeNull();
    expect(screen.queryByRole('button', { name: /a harbour at dawn/ })).toBeNull();
    view.unmount();

    mountPanel();
    expect(screen.queryByRole('searchbox')).toBeNull();
    expect(screen.getByRole('button', { name: 'Show the history' })).toBeTruthy();
  });

  it('still opens when localStorage throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    mountPanel();
    expect(screen.getByRole('searchbox', { name: 'Search the history' })).toBeTruthy();
  });
});

describe('StudioStage', () => {
  it('shows the gradient headline while nothing is selected', () => {
    render(<StudioStage record={undefined} onReuse={vi.fn()} />);
    const headline = screen.getByRole('heading', { name: 'Bring your idea to life' });
    expect(headline.className).toContain('studio-title');
  });

  it('counts a running job from when it was submitted, not from when the page opened', () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-09-17T10:01:05Z'));
    render(
      <StudioStage record={{ ...RUNNING, created_at: '2026-09-17T10:00:00Z' }} onReuse={vi.fn()} />,
    );
    expect(screen.getByRole('timer').textContent).toBe('1:05');
    vi.useRealTimers();
  });

  it('gives a failed record the sentence for its code, not the raw server string', () => {
    render(<StudioStage record={FAILED} onReuse={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain(
      'This deployment has no OpenRouter key',
    );
    expect(screen.getByRole('alert').textContent).not.toContain('credential missing');
  });

  it('refuses to reuse an outcome nobody knows, and says it may already be billed', () => {
    render(
      <StudioStage
        record={record({
          id: 'job-u',
          status: 'failed',
          prompt: 'a clip that vanished',
          error: { code: 'outcome_unknown', message: 'no terminal status' },
        })}
        onReuse={vi.fn()}
      />,
    );
    expect(screen.getByRole('alert').textContent).toContain('may already have been billed');
    // Reuse under that sentence is an offer to pay for the same clip twice.
    expect(screen.queryByRole('button', { name: /Reuse/ })).toBeNull();
  });

  it('still offers Reuse for a refusal that cost nothing', () => {
    render(<StudioStage record={FAILED} onReuse={vi.fn()} />);
    expect(screen.getByRole('button', { name: /Reuse/ })).toBeTruthy();
  });

  it('falls back to what the server said for a code minted after this build', () => {
    render(
      <StudioStage
        record={{ ...FAILED, error: { code: 'quarantined', message: 'Prompt was refused.' } }}
        onReuse={vi.fn()}
      />,
    );
    expect(screen.getByRole('alert').textContent).toContain('Prompt was refused.');
  });

  it('names a blocked prompt in Aura’s own words, not the provider’s', () => {
    render(
      <StudioStage
        record={{ ...FAILED, error: { code: 'content_blocked', message: 'policy_violation' } }}
        onReuse={vi.fn()}
      />,
    );
    expect(screen.getByRole('alert').textContent).toContain(
      'The provider blocked this prompt or these images.',
    );
    expect(screen.getByRole('alert').textContent).not.toContain('policy_violation');
  });

  it('offers Download and Reuse on a finished generation, with its cost', () => {
    const onReuse = vi.fn();
    render(<StudioStage record={DONE} onReuse={onReuse} />);
    expect(screen.getByRole('link', { name: /Download/ }).getAttribute('href')).toBe(
      '/api/assets/asset-1/download',
    );
    expect(screen.getByText('$0.12')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Reuse/ }));
    expect(onReuse.mock.calls[0]?.[0]).toMatchObject({ id: 'job-1' });
  });

  it('says the cost is unknown rather than printing a zero the bill will not match', () => {
    // Written out rather than overridden: `cost_usd` is absent here, which is what a record
    // the provider never priced looks like.
    const priceless: StudioRecord = {
      id: 'job-5',
      kind: 'image',
      status: 'completed',
      model: 'black-forest-labs/flux-3',
      prompt: 'a quiet room',
      used: { aspect_ratio: '1:1' },
      asset_id: 'asset-5',
      created_at: '2026-09-17T11:00:00Z',
    };
    render(<StudioStage record={priceless} onReuse={vi.fn()} />);
    expect(screen.getByText('Cost unknown')).toBeTruthy();
    expect(screen.queryByText('$0.00')).toBeNull();
  });
});
