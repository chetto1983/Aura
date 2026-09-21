import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { withLocalFonts, type FontLoadingRenderer } from '../videoflow';

const stylesheet = { outcome: 'load' as 'load' | 'error' };

beforeEach(() => {
  stylesheet.outcome = 'load';
  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: { load: vi.fn(() => Promise.resolve([])) },
  });
  vi.spyOn(document.head, 'appendChild').mockImplementation(<T extends Node>(node: T): T => {
    Node.prototype.appendChild.call(document.head, node);
    queueMicrotask(() => node.dispatchEvent(new Event(stylesheet.outcome)));
    return node;
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  document.head.querySelectorAll('link[data-local-font]').forEach((link) => {
    link.remove();
  });
});

describe('withLocalFonts', () => {
  it('serves a known family from our own origin instead of Google Fonts', async () => {
    const stockLoadFont = vi.fn(() => Promise.resolve());
    const patched = withLocalFonts<FontLoadingRenderer>({
      loadedFonts: {},
      loadFont: stockLoadFont,
    });
    await patched.loadFont('Atkinson Hyperlegible Next');

    expect(stockLoadFont).not.toHaveBeenCalled();
    expect(patched.loadedFonts['Atkinson Hyperlegible Next']).toBe('/fonts/atkinson.css');
    const link = document.head.querySelector<HTMLLinkElement>('link[data-local-font]');
    expect(link?.getAttribute('href')).toBe('/fonts/atkinson.css');
  });

  it('links a family once, however many layers ask for it', async () => {
    const patched = withLocalFonts<FontLoadingRenderer>({
      loadedFonts: {},
      loadFont: vi.fn(() => Promise.resolve()),
    });
    await patched.loadFont('Noto Sans');
    await patched.loadFont('Noto Sans');

    expect(document.head.querySelectorAll('link[data-local-font]')).toHaveLength(1);
  });

  it('lets a family be retried after its stylesheet failed', async () => {
    const patched = withLocalFonts<FontLoadingRenderer>({
      loadedFonts: {},
      loadFont: vi.fn(() => Promise.resolve()),
    });
    stylesheet.outcome = 'error';
    await patched.loadFont('Atkinson Hyperlegible Next');
    expect(patched.loadedFonts).toEqual({});
    expect(document.head.querySelectorAll('link[data-local-font]')).toHaveLength(0);

    stylesheet.outcome = 'load';
    await patched.loadFont('Atkinson Hyperlegible Next');
    expect(patched.loadedFonts['Atkinson Hyperlegible Next']).toBe('/fonts/atkinson.css');
    expect(document.head.querySelectorAll('link[data-local-font]')).toHaveLength(1);
  });

  it('leaves an unknown family to the fallback stack rather than fetching it', async () => {
    const patched = withLocalFonts<FontLoadingRenderer>({
      loadedFonts: {},
      loadFont: vi.fn(() => Promise.resolve()),
    });
    await patched.loadFont('Comic Sans MS');
    await patched.loadFont('toString');

    expect(patched.loadedFonts).toEqual({});
    expect(document.head.querySelectorAll('link[data-local-font]')).toHaveLength(0);
  });
});
