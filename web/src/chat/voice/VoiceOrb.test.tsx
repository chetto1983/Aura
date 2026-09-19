import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { VoiceOrb } from './VoiceOrb';
import type { VoicePhase } from './voiceSessionMachine';

// The orb is decoration over a role="status" line, so what is worth pinning is exactly
// that: it publishes the phase for CSS, carries the level as a custom property rather
// than as React style churn, and stays out of the accessibility tree.

describe('VoiceOrb', () => {
  it.each<VoicePhase>(['idle', 'listening', 'transcribing', 'thinking', 'speaking', 'error'])(
    'publishes the %s phase to CSS and hides itself from assistive tech',
    (phase) => {
      render(<VoiceOrb phase={phase} level={0.5} />);
      const orb = screen.getByTestId('voice-orb');
      expect(orb.getAttribute('data-phase')).toBe(phase);
      expect(orb.getAttribute('aria-hidden')).toBe('true');
      expect(orb.style.getPropertyValue('--orb-level')).toBe('0.5');
    },
  );

  it('clamps a level outside 0..1 and survives a non-finite sample', () => {
    const { rerender } = render(<VoiceOrb phase="listening" level={4} />);
    expect(screen.getByTestId('voice-orb').style.getPropertyValue('--orb-level')).toBe('1');
    rerender(<VoiceOrb phase="listening" level={-2} />);
    expect(screen.getByTestId('voice-orb').style.getPropertyValue('--orb-level')).toBe('0');
    rerender(<VoiceOrb phase="listening" level={Number.NaN} />);
    expect(screen.getByTestId('voice-orb').style.getPropertyValue('--orb-level')).toBe('0');
  });
});
