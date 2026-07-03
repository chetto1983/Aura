import { afterEach, beforeEach, describe, expect, it } from 'vitest';
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
    localStorage.removeItem(PREF_KEY);
  });

  it('shows a safe lifecycle cue when reasoning has started but no text has streamed yet', () => {
    render(<ReasoningDrawer text="" />);
    expect(screen.getByRole('button')).toBeTruthy();
    expect(screen.getByText('Thinking...')).toBeTruthy();
  });

  it('shows the reasoning text by default (builder default = shown)', () => {
    render(<ReasoningDrawer text="step one then step two" />);
    const toggle = screen.getByRole('button');
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('step one then step two')).toBeTruthy();
  });

  it('toggling hides the text and persists the preference', () => {
    render(<ReasoningDrawer text="hidden cot" />);
    const toggle = screen.getByRole('button');
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-pressed')).toBe('false');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('hidden cot')).toBeNull();
    // Persisted to localStorage as "0" (hidden).
    expect(localStorage.getItem(PREF_KEY)).toBe('0');
    expect(readReasoningPref()).toBe(false);
  });

  it('honors a persisted hidden preference on mount', () => {
    localStorage.setItem(PREF_KEY, '0');
    render(<ReasoningDrawer text="should start collapsed" />);
    const toggle = screen.getByRole('button');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('should start collapsed')).toBeNull();
  });

  it('toggling from hidden back to shown re-reveals and persists "1"', () => {
    localStorage.setItem(PREF_KEY, '0');
    render(<ReasoningDrawer text="reveal me" />);
    fireEvent.click(screen.getByRole('button'));
    expect(screen.getByText('reveal me')).toBeTruthy();
    expect(localStorage.getItem(PREF_KEY)).toBe('1');
  });

  it('readReasoningPref defaults to shown when storage is empty', () => {
    expect(readReasoningPref()).toBe(true);
  });
});
