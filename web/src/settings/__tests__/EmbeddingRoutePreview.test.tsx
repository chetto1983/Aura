import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { EmbeddingRoutePreviewCard } from '../EmbeddingRoutePreview';
import type { EmbeddingRoutePreview } from '../embeddingSpaceApi';

function preview(overrides: Partial<EmbeddingRoutePreview> = {}): EmbeddingRoutePreview {
  return {
    space: 'es1-target',
    space_label: 'openrouter vendor/embed, 768d, recipe 1',
    memory_space: 'es1-target',
    native_width: 768,
    dimensions: 768,
    width_warning: false,
    chars_per_second: 3000,
    input_limit: 8192,
    work: { types: [{ type: 'Passage', rows: 20, chars: 30000 }], passages_over_limit: 0 },
    tokens: 10000,
    cost_usd: 12.5,
    local: false,
    duration_seconds: 10,
    floors_calibrated: true,
    refusals: [],
    ...overrides,
  };
}

const applyButton = () => screen.getByRole('button', { name: 'Apply and restart' });

describe('EmbeddingRoutePreviewCard', () => {
  it('prices the work and applies only once the operator confirms', () => {
    const onApply = vi.fn();
    render(<EmbeddingRoutePreviewCard preview={preview()} applying={false} onApply={onApply} />);
    expect(screen.getByText('about $12.50')).toBeTruthy();
    expect(screen.getByText('20 records, 30,000 characters (about 10,000 tokens)')).toBeTruthy();
    expect(screen.getByText('about 1 min')).toBeTruthy();
    expect(screen.queryByText(/Matryoshka/)).toBeNull();
    expect(applyButton().hasAttribute('disabled')).toBe(true);
    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.click(applyButton());
    expect(onApply).toHaveBeenCalledTimes(1);
  });

  it('says a fraction of a cent, an unknown price and a local model plainly', () => {
    const { rerender } = render(
      <EmbeddingRoutePreviewCard
        preview={preview({ cost_usd: 0.00042 })}
        applying={false}
        onApply={vi.fn()}
      />,
    );
    expect(screen.getByText('about $0.00042')).toBeTruthy();
    rerender(
      <EmbeddingRoutePreviewCard
        preview={preview({ cost_usd: null })}
        applying={false}
        onApply={vi.fn()}
      />,
    );
    expect(screen.getByText(/Unknown: the catalogue publishes no price/)).toBeTruthy();
    rerender(
      <EmbeddingRoutePreviewCard
        preview={preview({ local: true, cost_usd: 0 })}
        applying={false}
        onApply={vi.fn()}
      />,
    );
    expect(screen.getByText('No charge: a local model')).toBeTruthy();
  });

  it('warns about truncation, cut passages and uncalibrated floors', () => {
    render(
      <EmbeddingRoutePreviewCard
        preview={preview({
          native_width: 4096,
          width_warning: true,
          floors_calibrated: false,
          work: { types: [], passages_over_limit: 3 },
        })}
        applying={false}
        onApply={vi.fn()}
      />,
    );
    expect(screen.getByText(/Matryoshka/)).toBeTruthy();
    expect(screen.getByText('3 stored passages exceed it and will be cut')).toBeTruthy();
    expect(screen.getByText(/uncalibrated/)).toBeTruthy();
  });

  it('shows the refusals and never lets a refused route be applied', () => {
    render(
      <EmbeddingRoutePreviewCard
        preview={preview({
          space: '',
          refusals: [
            { code: 'key_missing' },
            { code: 'probe_failed', detail: 'HTTP 503' },
            { code: 'something_new', detail: 'a reason this cockpit predates' },
          ],
        })}
        applying={false}
        onApply={vi.fn()}
      />,
    );
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('needs an OpenRouter key');
    expect(alert.textContent).toContain('The route did not answer: HTTP 503');
    expect(alert.textContent).toContain('a reason this cockpit predates');
    expect(screen.queryByText('Target space')).toBeNull();
    expect(screen.getByRole('checkbox').hasAttribute('disabled')).toBe(true);
    expect(applyButton().hasAttribute('disabled')).toBe(true);
  });

  it('shows the apply in flight', () => {
    render(<EmbeddingRoutePreviewCard preview={preview()} applying onApply={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Applying…' });
    expect(button.getAttribute('aria-busy')).toBe('true');
  });
});
