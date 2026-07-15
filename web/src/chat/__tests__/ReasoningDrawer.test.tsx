import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ReasoningDrawer } from '../ReasoningDrawer';
import { readReasoningPref } from '../reasoningPref';

const PREF_KEY = 'aura.chat.reasoning.shown';

describe('ReasoningDrawer (D-01)', () => {
  beforeEach(() => {
    localStorage.removeItem(PREF_KEY);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.removeItem(PREF_KEY);
  });

  it('shows a safe lifecycle cue when reasoning has started but no text has streamed yet', () => {
    render(<ReasoningDrawer text="" />);
    const toggle = screen.getByRole('button', { name: 'Show reasoning' });
    const body = screen.getByText('Thinking...');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(body.hidden).toBe(true);
    fireEvent.click(toggle);
    expect(screen.getByRole('button', { name: 'Hide reasoning' })).toBeTruthy();
    expect(body.hidden).toBe(false);
  });

  it.each([
    [null, false],
    ['invalid', false],
    ['0', false],
    ['1', true],
  ])('reads %s as %s', (stored, expected) => {
    if (stored === null) localStorage.removeItem(PREF_KEY);
    else localStorage.setItem(PREF_KEY, stored);
    expect(readReasoningPref()).toBe(expected);
  });

  it('defaults hidden when storage cannot be read', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked');
    });
    expect(readReasoningPref()).toBe(false);
    get.mockRestore();
  });

  it('persists toggles with the existing 1/0 encoding', () => {
    render(<ReasoningDrawer text="encoded trace" />);
    const toggle = screen.getByRole('button', { name: 'Show reasoning' });
    fireEvent.click(toggle);
    expect(localStorage.getItem(PREF_KEY)).toBe('1');
    fireEvent.click(toggle);
    expect(localStorage.getItem(PREF_KEY)).toBe('0');
  });

  it('keeps the current-session toggle when persistence fails', () => {
    localStorage.setItem(PREF_KEY, '1');
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota');
    });
    render(<ReasoningDrawer text="private trace" />);
    fireEvent.click(screen.getByRole('button', { name: 'Hide reasoning' }));
    expect(screen.getByText('private trace').hidden).toBe(true);
    expect(localStorage.getItem(PREF_KEY)).toBe('1');
    set.mockRestore();
  });

  it('uses the last readable explicit value on the next mount after a failed write', () => {
    localStorage.setItem(PREF_KEY, '1');
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota');
    });
    const first = render(<ReasoningDrawer text="first mount" />);
    fireEvent.click(screen.getByRole('button', { name: 'Hide reasoning' }));
    first.unmount();
    render(<ReasoningDrawer text="second mount" />);
    expect(screen.getByRole('button').getAttribute('aria-expanded')).toBe('true');
    set.mockRestore();
  });

  it('gives every disclosure a resolving unique body id without aria-pressed', () => {
    const { container } = render(
      <>
        <ReasoningDrawer text="one" />
        <ReasoningDrawer text="two" />
      </>,
    );
    const buttons = screen.getAllByRole('button');
    const ids = buttons.map((button) => button.getAttribute('aria-controls'));
    expect(new Set(ids).size).toBe(2);
    for (const [index, button] of buttons.entries()) {
      const id = ids[index];
      expect(button.hasAttribute('aria-pressed')).toBe(false);
      expect(id).not.toBeNull();
      expect(container.querySelector(`[id="${id ?? ''}"]`)).not.toBeNull();
    }
  });

  it('uses the compact touch target and disclosure border treatment', () => {
    render(<ReasoningDrawer text="styled trace" />);
    const toggle = screen.getByRole('button');
    const body = screen.getByText('styled trace');
    expect(toggle.className).toContain('min-h-11');
    expect(toggle.className).toContain('px-2');
    expect(toggle.className).toContain('text-xs');
    expect(toggle.className).toContain('text-text-muted');
    expect(toggle.className).toContain('hover:text-text');
    expect(body.className).toContain('border-s');
    expect(body.className).toContain('border-border');
  });
});
