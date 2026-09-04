import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { ModelPicker } from '../ModelPicker';
import type { ModelCatalogState } from '../useModelCatalog';
import type { LLMCatalogModel } from '../settingsApi';

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
    <ModelPicker id="setting-AURA_LLM_MODEL" value={value} catalog={state} onChange={onChange} />,
  );
  return onChange;
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
