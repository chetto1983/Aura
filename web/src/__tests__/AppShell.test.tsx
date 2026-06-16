import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
// RED until 23-02 creates src/AppShell.tsx — the import itself fails today, which
// is the Wave-0 contract: the gate test exists before the component it guards.
import { AppShell } from '../AppShell';

// The marketing-hero copy this operator console must NOT ship (ux-spec §350 / SC4).
const MARKETING_HERO_BLOCKLIST = [
  /get started for free/i,
  /sign up today/i,
  /trusted by/i,
  /supercharge/i,
  /unlock your/i,
  /the future of/i,
];

describe('AppShell', () => {
  it('renders the Aura brand mark', () => {
    render(<AppShell />);
    expect(screen.getByRole('img', { name: /aura/i })).toBeTruthy();
  });

  it('ships no marketing-hero copy in the primary viewport', () => {
    const { container } = render(<AppShell />);
    const text = container.textContent ?? '';
    for (const pattern of MARKETING_HERO_BLOCKLIST) {
      expect(text).not.toMatch(pattern);
    }
  });
});
