import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { StudioBar } from '../StudioBar';
import type { StudioModel } from '../studioApi';
import { cheapestOptions, type StudioDraft } from '../studioForm';
import { formatEstimate, modelPrice } from '../studioPrice';

// StudioBar is the surface the money is spent from, so what is pinned here is what an
// operator reads before pressing Generate: the pill that summarizes the request, the price
// of the exact cell being bought, and the fact that a control the model does not declare is
// not on screen at all.

// veo-3.1-lite as measured on 2026-09-17: $0.0297/s silent at 720p — 4 s of it is $0.1188,
// which the button has to round to $0.12 rather than print at four digits.
const VEO: StudioModel = {
  id: 'google/veo-3.1-lite',
  name: 'Veo 3.1 Lite',
  description: 'Fast clips with optional sound',
  durations: [4, 8],
  resolutions: ['720p', '1080p'],
  aspect_ratios: ['16:9', '9:16'],
  frame_images: ['first_frame', 'last_frame'],
  audio: true,
  seed: true,
  prices: [
    { resolution: '720p', audio: false, usd_per_second: 0.0297 },
    { resolution: '720p', audio: true, usd_per_second: 0.05 },
    { resolution: '1080p', audio: false, usd_per_second: 0.08 },
    { resolution: '1080p', audio: true, usd_per_second: 0.12 },
  ],
};

const PLAIN_VIDEO: StudioModel = {
  id: 'plain/video',
  name: 'Plain Video',
  durations: [4],
  resolutions: ['720p'],
  aspect_ratios: ['16:9'],
  audio: false,
  seed: false,
};

const FLUX: StudioModel = {
  id: 'black-forest-labs/flux-3',
  name: 'FLUX 3',
  description: 'Reference-driven stills',
  aspect_ratios: ['1:1', '3:2'],
  audio: false,
  seed: false,
  reference_max: 2,
  image_min_usd: 0.04,
  image_max_usd: 0.06,
};

function draftFor(model: StudioModel, kind: 'image' | 'video'): StudioDraft {
  return {
    kind,
    model: model.id,
    prompt: '',
    options: cheapestOptions(model),
    images: [],
    endFrame: undefined,
  };
}

interface BarOptions {
  readonly kind?: 'image' | 'video';
  readonly models?: readonly StudioModel[];
  readonly submitting?: boolean;
}

function mountBar(model: StudioModel, options: BarOptions = {}) {
  const kind = options.kind ?? 'video';
  const onSubmit = vi.fn();
  const onKindChange = vi.fn();
  const onModelChange = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Host() {
    const [draft, setDraft] = useState(() => draftFor(model, kind));
    return (
      <StudioBar
        draft={draft}
        model={model}
        models={options.models ?? [model]}
        submitting={options.submitting ?? false}
        onDraftChange={setDraft}
        onKindChange={onKindChange}
        onModelChange={onModelChange}
        onSubmit={onSubmit}
      />
    );
  }

  render(
    <QueryClientProvider client={client}>
      <Host />
    </QueryClientProvider>,
  );
  return { onSubmit, onKindChange, onModelChange };
}

function type(text: string) {
  fireEvent.change(screen.getByRole('textbox', { name: 'Prompt' }), { target: { value: text } });
}

describe('studioPrice', () => {
  it('rounds a bill to cents but keeps a sub-cent rate readable', () => {
    expect(formatEstimate(0.1188)).toBe('0.12');
    // Two decimals would print $0.00 over a request that is not free.
    expect(formatEstimate(0.004)).toBe('0.004');
  });

  it('spans a video price matrix and names how each model is metered', () => {
    expect(modelPrice(VEO)).toEqual({ unit: 'second', amount: '0.0297–0.12' });
    expect(modelPrice(FLUX)).toEqual({ unit: 'image', amount: '0.04–0.06' });
    expect(
      modelPrice({ id: 'x', audio: false, seed: false, image_token_min_per_1m: 5 }),
    ).toBeUndefined();
  });
});

describe('StudioBar', () => {
  it('summarizes the request on the options pill and prices that exact cell', () => {
    mountBar(VEO);
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 720p · 4s');
    expect(screen.getByText('≈ $0.12')).toBeTruthy();
  });

  it('reprices when 1080p and Sound are turned on', () => {
    mountBar(VEO);
    fireEvent.click(screen.getByRole('button', { name: 'Options' }));
    fireEvent.click(screen.getByRole('radio', { name: '1080p' }));
    fireEvent.keyDown(document.body, { key: 'Escape' });
    fireEvent.click(screen.getByRole('button', { name: 'Advanced' }));
    fireEvent.click(screen.getByRole('switch', { name: 'Sound' }));
    // 1080p with sound is $0.12/s in the matrix: four seconds of it is $0.48, not $0.12.
    expect(screen.getByText('≈ $0.48')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 1080p · 4s');
  });

  it('says the price is unknown rather than showing $0 for an unpriced model', () => {
    mountBar(PLAIN_VIDEO);
    expect(screen.getByText('price not published')).toBeTruthy();
    expect(screen.queryByText('≈ $0.00')).toBeNull();
  });

  it('hides the duration and the Advanced pill for an image model', () => {
    mountBar(FLUX, { kind: 'image' });
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('1:1');
    expect(screen.queryByRole('button', { name: 'Advanced' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Options' }));
    expect(screen.getByRole('radiogroup', { name: 'Aspect ratio' })).toBeTruthy();
    expect(screen.queryByRole('group', { name: 'Duration' })).toBeNull();
    expect(screen.queryByRole('radiogroup', { name: 'Resolution' })).toBeNull();
  });

  it('offers no Advanced pill for a video model that declares neither seed nor sound', () => {
    mountBar(PLAIN_VIDEO);
    expect(screen.queryByRole('button', { name: 'Advanced' })).toBeNull();
  });

  it('shows an image model only its ratios, however much its row declares', () => {
    // A catalog row is free to carry video axes on a model the image route will be called
    // for; requestBody drops every one of them, so offering the controls would be offering
    // choices that never leave the browser.
    const OVERDECLARED: StudioModel = {
      id: 'odd/image',
      name: 'Overdeclared',
      aspect_ratios: ['1:1', '3:2'],
      resolutions: ['720p', '1080p'],
      durations: [4, 8],
      audio: true,
      seed: true,
      reference_max: 1,
      image_min_usd: 0.03,
      image_max_usd: 0.03,
    };
    mountBar(OVERDECLARED, { kind: 'image' });

    expect(screen.queryByRole('button', { name: 'Advanced' })).toBeNull();
    // The pill reads what will be sent: the ratio, not a resolution the body omits.
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('1:1');
    fireEvent.click(screen.getByRole('button', { name: 'Options' }));
    expect(screen.getByRole('radiogroup', { name: 'Aspect ratio' })).toBeTruthy();
    expect(screen.queryByRole('radiogroup', { name: 'Resolution' })).toBeNull();
    expect(screen.queryByRole('group', { name: 'Duration' })).toBeNull();
    expect(screen.queryByRole('slider')).toBeNull();
  });

  it('shows each model row with its name, its description and its price', () => {
    mountBar(VEO, { models: [VEO, PLAIN_VIDEO] });
    // The pill reads the name, never the id.
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Veo 3.1 Lite');
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).not.toContain('google/');
    fireEvent.click(screen.getByRole('combobox', { name: 'Model' }));
    const row = screen.getByRole('option', { name: /Veo 3.1 Lite/ });
    expect(row.textContent).toContain('Fast clips with optional sound');
    expect(row.textContent).toContain('$0.0297–0.12 per second');
    // A model the catalog never priced states no price at all.
    expect(screen.getByRole('option', { name: /Plain Video/ }).textContent).not.toContain('$');
  });

  it('keeps Generate disabled until a prompt is typed', () => {
    const { onSubmit } = mountBar(VEO);
    const generate = screen.getByRole('button', { name: /Generate/ });
    expect(generate.hasAttribute('disabled')).toBe(true);
    fireEvent.click(generate);
    expect(onSubmit).not.toHaveBeenCalled();

    type('a harbour at dawn');
    expect(screen.getByRole('button', { name: /Generate/ }).hasAttribute('disabled')).toBe(false);
  });

  it('expands and collapses the mobile composer without changing the draft', () => {
    mountBar(VEO);
    type('a harbour at dawn');
    const expand = screen.getByRole('button', { name: 'Expand the composer' });
    fireEvent.click(expand);

    const collapse = screen.getByRole('button', { name: 'Collapse the composer' });
    expect(collapse.getAttribute('aria-pressed')).toBe('true');
    expect(collapse.closest('form')?.dataset.expanded).toBe('true');
    expect(screen.getByRole<HTMLTextAreaElement>('textbox', { name: 'Prompt' }).value).toBe(
      'a harbour at dawn',
    );

    fireEvent.click(collapse);
    expect(screen.getByRole('button', { name: 'Expand the composer' })).toBeTruthy();
  });

  it('submits on Ctrl+Enter, once, and not on a bare Enter', () => {
    const { onSubmit } = mountBar(VEO);
    type('a harbour at dawn');
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    fireEvent.keyDown(prompt, { key: 'Enter' });
    // A bare Enter is a newline in a prompt box, not a purchase.
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.keyDown(prompt, { key: 'Enter', ctrlKey: true });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it('refuses Ctrl+Enter while the previous request is still in flight', () => {
    const { onSubmit } = mountBar(VEO, { submitting: true });
    type('a harbour at dawn');
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Prompt' }), {
      key: 'Enter',
      ctrlKey: true,
    });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: /Sending/ }).hasAttribute('disabled')).toBe(true);
  });

  it('switches mode through the caller, which owns the reconcile', () => {
    const { onKindChange } = mountBar(VEO);
    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    expect(onKindChange).toHaveBeenCalledWith('image');
  });

  it('offers the end-frame tile only once a start frame is set', () => {
    mountBar(VEO);
    expect(screen.queryByRole('button', { name: 'End frame' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Start frame' })).toBeTruthy();
  });

  it('disables the image tile, with the reason, for a model that takes none', () => {
    mountBar(PLAIN_VIDEO);
    const tile = screen.getByRole('button', { name: 'Start frame' });
    expect(tile.getAttribute('aria-disabled')).toBe('true');
  });
});
