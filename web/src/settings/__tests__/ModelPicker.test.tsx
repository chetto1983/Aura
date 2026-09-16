import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { ModelPicker } from '../ModelPicker';
import type { ModelCatalogState } from '../useModelCatalog';
import type { LLMCatalogModel } from '../settingsApi';
import { modelMeta } from '../modelCatalogFormat';
import type { ImageCatalogModel, VideoCatalogModel } from '../mediaModelCatalog';
import { imageModelMeta, videoModelMeta, type MediaLabels } from '../mediaModelCatalogFormat';

const OPENROUTER: readonly LLMCatalogModel[] = [
  {
    id: 'z-ai/glm-5.3',
    context_window: 204_800,
    input_per_1m: 0.14,
    output_per_1m: 0.28,
    has_price: true,
  },
  {
    id: 'z-ai/glm-5.3-flash',
    context_window: 204_800,
    input_per_1m: 0.02,
    output_per_1m: 0.04,
    has_price: true,
  },
  {
    id: 'deepseek/deepseek-v4-flash',
    context_window: 1_000_000,
    input_per_1m: 0.2,
    output_per_1m: 0.8,
    has_price: true,
  },
];

const LOCAL: readonly LLMCatalogModel[] = [
  { id: 'gemma-4-12b', context_window: 131_072, has_price: false },
];

function catalog(over: Partial<ModelCatalogState> = {}): ModelCatalogState {
  return {
    models: OPENROUTER,
    status: 'ready',
    error: undefined,
    reload: vi.fn(),
    ...over,
  };
}

function renderPicker(value: string, state: ModelCatalogState) {
  const onChange = vi.fn();
  render(
    <ModelPicker
      id="setting-AURA_LLM_MODEL"
      value={value}
      catalog={state}
      onChange={onChange}
      formatRow={modelMeta}
    />,
  );
  return onChange;
}

const MEDIA_LABELS: MediaLabels = {
  references: (max) => `up to ${String(max)} reference images`,
  duration: (min, max) => `${String(min)}–${String(max)} s`,
  imageToVideo: 'image-to-video',
  imageTokens: (price) => `${price}/M image tokens`,
};

// Fixture rows shaped like the catalogue measured during planning: MAI prices output
// per token (so no per-image price), Hailuo per second across two resolutions.
const IMAGE_MODELS: readonly ImageCatalogModel[] = [
  { kind: 'image', id: 'microsoft/mai-image-2.6', has_price: false, reference_max: 5 },
  {
    kind: 'image',
    id: 'black-forest-labs/flux-3-pro',
    has_price: true,
    image_min_usd: 0.04,
    image_max_usd: 0.06,
    reference_max: 8,
  },
  {
    kind: 'image',
    id: 'black-forest-labs/flux-3-schnell',
    has_price: true,
    image_min_usd: 0,
    image_max_usd: 0,
  },
];

const VIDEO_MODELS: readonly VideoCatalogModel[] = [
  {
    kind: 'video',
    id: 'minimax/hailuo-3-max',
    has_price: true,
    second_min_usd: 0.05,
    second_max_usd: 0.08,
    duration_min: 5,
    duration_max: 15,
    resolutions: ['768p', '480p'],
    image_to_video: true,
  },
  { kind: 'video', id: 'google/veo-4', has_price: false, duration_min: 8, duration_max: 8 },
];

function mediaCatalog<M>(
  models: readonly M[],
  over: Partial<ModelCatalogState<M>> = {},
): ModelCatalogState<M> {
  return { models, status: 'ready', error: undefined, reload: vi.fn(), ...over };
}

function renderImagePicker(value: string, state: ModelCatalogState<ImageCatalogModel>) {
  const onChange = vi.fn();
  render(
    <ModelPicker
      id="setting-AURA_IMAGE_MODEL"
      value={value}
      catalog={state}
      onChange={onChange}
      formatRow={(model) => imageModelMeta(model, MEDIA_LABELS)}
    />,
  );
  return onChange;
}

function renderVideoPicker(value: string, state: ModelCatalogState<VideoCatalogModel>) {
  const onChange = vi.fn();
  render(
    <ModelPicker
      id="setting-AURA_VIDEO_MODEL"
      value={value}
      catalog={state}
      onChange={onChange}
      formatRow={(model) => videoModelMeta(model, MEDIA_LABELS)}
    />,
  );
  return onChange;
}

function optionText(id: string): string | null | undefined {
  return screen.getAllByRole('option').find((option) => option.textContent?.startsWith(id))
    ?.textContent;
}

describe('ModelPicker', () => {
  it('groups the catalogue by vendor and shows each row’s window and rate', () => {
    renderPicker('z-ai/glm-5.3', catalog());
    fireEvent.click(screen.getByRole('combobox'));

    // OpenRouter ids are `vendor/name`, so the two z-ai models sit under one heading.
    expect(screen.getByText('z-ai')).toBeTruthy();
    expect(screen.getByText('deepseek')).toBeTruthy();
    // The row renders id then description with no separator node, so its textContent is
    // "z-ai/glm-5.3" + "205K ctx · ..." -- matched here as one string to pick the exact
    // model rather than its -flash sibling.
    const row = screen
      .getAllByRole('option')
      .find((option) => option.textContent?.startsWith('z-ai/glm-5.3205K'));
    expect(row?.textContent).toContain('205K ctx');
    expect(row?.textContent).toContain('$0.14 / $0.28');
  });

  it('says a local model has no token charge instead of a fabricated $0', () => {
    renderPicker('gemma-4-12b', catalog({ models: LOCAL }));
    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.getByRole('option', { name: /gemma-4-12b/ }).textContent).toContain(
      'no token charge',
    );
  });

  it('picks a model from the list', () => {
    const onChange = renderPicker('z-ai/glm-5.3', catalog());
    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.click(screen.getByRole('option', { name: /deepseek-v4-flash/ }));
    expect(onChange).toHaveBeenCalledWith('deepseek/deepseek-v4-flash');
  });

  it('commits an id the endpoint does not publish, because llama.cpp serves aliases', () => {
    const onChange = renderPicker('', catalog({ models: LOCAL }));
    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'qwen2.5-coder:14b' },
    });
    fireEvent.click(screen.getByText(/Use "qwen2.5-coder:14b" as typed/));
    expect(onChange).toHaveBeenCalledWith('qwen2.5-coder:14b');
  });

  it('keeps the running model selectable and marks it once the endpoint has answered', () => {
    renderPicker('gemma-4-12b-alias', catalog({ models: LOCAL }));
    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.getByRole('option', { name: /gemma-4-12b-alias/ }).textContent).toContain(
      'not published by this endpoint',
    );
  });

  it('does not call the running model unpublished while the probe is still in flight', () => {
    // Empty-because-loading is not empty-because-absent: claiming the latter would put a
    // false label under the model that is actually serving, on every page load.
    renderPicker('gemma-4-12b', catalog({ models: [], status: 'loading' }));
    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.queryByText(/not published by this endpoint/)).toBeNull();
    expect(screen.getByText(/Asking the endpoint what it serves/)).toBeTruthy();
  });

  it('shows why a probe failed and still lets the operator type', () => {
    renderPicker(
      'gemma-4-12b',
      catalog({ models: [], status: 'error', error: 'GET /models returned 401' }),
    );
    expect(screen.getByText(/401/)).toBeTruthy();
  });

  it('re-probes on demand', () => {
    const reload = vi.fn();
    renderPicker('z-ai/glm-5.3', catalog({ reload }));
    fireEvent.click(screen.getByRole('button', { name: /refresh/i }));
    expect(reload).toHaveBeenCalled();
  });

  it('reports how many models the endpoint publishes', () => {
    renderPicker('z-ai/glm-5.3', catalog());
    expect(screen.getByText(/3 models published here/)).toBeTruthy();
  });
});

describe('ModelPicker over the media catalogues', () => {
  it('writes each image row with its per-image price and reference limit, grouped by vendor', () => {
    renderImagePicker('microsoft/mai-image-2.6', mediaCatalog(IMAGE_MODELS));
    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.getByText('microsoft')).toBeTruthy();
    expect(screen.getByText('black-forest-labs')).toBeTruthy();
    expect(optionText('black-forest-labs/flux-3-pro')).toBe(
      'black-forest-labs/flux-3-pro$0.04–$0.06/image · up to 8 reference images',
    );
    expect(optionText('black-forest-labs/flux-3-schnell')).toBe(
      'black-forest-labs/flux-3-schnell$0/image',
    );
    // MAI bills output per token: no per-image price is honest, the LLM "no token charge"
    // label would be a lie, and the reference limit is still worth showing.
    expect(optionText('microsoft/mai-image-2.6')).toBe(
      'microsoft/mai-image-2.6up to 5 reference images',
    );
  });

  it('writes each video row with its $/s range, durations, resolutions and image-to-video', () => {
    renderVideoPicker('minimax/hailuo-3-max', mediaCatalog(VIDEO_MODELS));
    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.getByText('minimax')).toBeTruthy();
    expect(screen.getByText('google')).toBeTruthy();
    expect(optionText('minimax/hailuo-3-max')).toBe(
      'minimax/hailuo-3-max$0.05–$0.08/s · 5–15 s · 768p, 480p · image-to-video',
    );
    expect(optionText('google/veo-4')).toBe('google/veo-48–8 s');
  });

  it('picks a media model from the list', () => {
    const onChange = renderVideoPicker('minimax/hailuo-3-max', mediaCatalog(VIDEO_MODELS));
    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.click(screen.getByRole('option', { name: /google\/veo-4/ }));
    expect(onChange).toHaveBeenCalledWith('google/veo-4');
  });

  it('commits a typed image model id the catalogue does not list', () => {
    const onChange = renderImagePicker('', mediaCatalog(IMAGE_MODELS));
    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'vendor/private-image-model' },
    });
    fireEvent.click(screen.getByText(/Use "vendor\/private-image-model" as typed/));
    expect(onChange).toHaveBeenCalledWith('vendor/private-image-model');
  });

  it('keeps a saved video model the catalogue no longer lists', () => {
    renderVideoPicker('retired/video-model', mediaCatalog(VIDEO_MODELS));
    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.getByRole('option', { name: /retired\/video-model/ }).textContent).toContain(
      'not published by this endpoint',
    );
  });

  it('narrows the list to what the search matches and counts the whole catalogue', () => {
    renderImagePicker('microsoft/mai-image-2.6', mediaCatalog(IMAGE_MODELS));
    expect(screen.getByText(/3 models published here/)).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'flux-3-pro' },
    });
    const listed = screen.getAllByRole('option').map((option) => option.textContent);
    expect(listed).toContain(
      'black-forest-labs/flux-3-pro$0.04–$0.06/image · up to 8 reference images',
    );
    expect(listed.some((text) => (text ?? '').includes('mai-image'))).toBe(false);
    expect(listed.some((text) => (text ?? '').includes('flux-3-schnell'))).toBe(false);
  });

  it('shows why the catalogue failed and still accepts a typed id', () => {
    const onChange = renderVideoPicker(
      'minimax/hailuo-3-max',
      mediaCatalog<VideoCatalogModel>([], {
        status: 'error',
        error: 'media models are listed only on the OpenRouter route',
      }),
    );
    expect(screen.getByText(/only on the OpenRouter route/)).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox'));
    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'vendor/typed-video' },
    });
    fireEvent.click(screen.getByText(/Use "vendor\/typed-video" as typed/));
    expect(onChange).toHaveBeenCalledWith('vendor/typed-video');
  });

  it('asks the catalogue again on Refresh', () => {
    const reload = vi.fn();
    renderImagePicker('microsoft/mai-image-2.6', mediaCatalog(IMAGE_MODELS, { reload }));
    fireEvent.click(screen.getByRole('button', { name: /refresh/i }));
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
