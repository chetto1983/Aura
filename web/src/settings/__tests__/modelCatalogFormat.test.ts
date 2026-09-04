import { describe, expect, it } from 'vitest';
import { formatContext, formatRate, modelMeta } from '../modelCatalogFormat';

describe('formatContext', () => {
  it.each([
    [2_000_000, '2M'],
    [1_500_000, '1.5M'],
    [12_000_000, '12M'],
    [1_000_000, '1M'],
    [204_800, '205K'],
    [131_072, '131K'],
    [1_000, '1K'],
    [512, '512'],
    [0, '0'],
  ])('writes %d tokens as %s', (tokens, want) => {
    expect(formatContext(tokens)).toBe(want);
  });
});

describe('formatRate', () => {
  it.each([
    [0, '$0'],
    [0.14, '$0.14'],
    [12.5, '$12.50'],
    // Under a cent, $0.00 would read as free — the rate keeps the digits it needs.
    [0.000015, '$0.000015'],
    [0.009, '$0.0090'],
  ])('writes %d per 1M as %s', (rate, want) => {
    expect(formatRate(rate)).toBe(want);
  });
});

describe('modelMeta', () => {
  const free = 'no token charge';

  it('pairs the window with the published rates', () => {
    expect(
      modelMeta(
        {
          id: 'z-ai/glm-5.3',
          context_window: 204_800,
          input_per_1m: 0.14,
          output_per_1m: 0.28,
          has_price: true,
        },
        free,
      ),
    ).toBe('205K ctx · $0.14 / $0.28');
  });

  it('says a local model costs nothing per token rather than showing $0', () => {
    expect(modelMeta({ id: 'gemma-4-12b', context_window: 131_072, has_price: false }, free)).toBe(
      `131K ctx · ${free}`,
    );
  });

  it('omits a window the endpoint does not publish', () => {
    // Ollama's /v1/models lists ids alone: no window, and no invented "0 ctx".
    expect(modelMeta({ id: 'qwen4:14b', has_price: false }, free)).toBe(free);
    expect(modelMeta({ id: 'qwen4:14b', context_window: 0, has_price: false }, free)).toBe(free);
  });

  it('treats a priced model with absent rate fields as $0 rather than crashing', () => {
    expect(modelMeta({ id: 'openrouter/auto', has_price: true }, free)).toBe('$0 / $0');
  });
});
