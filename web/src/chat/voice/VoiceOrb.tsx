import type { CSSProperties } from 'react';
import { AlertTriangle, AudioLines, Ear, Loader2, Sparkles } from 'lucide-react';
import type { VoicePhase } from './voiceSessionMachine';

// VoiceOrb — the circle the hands-free overlay is built around. Presentational and
// pure: it takes the phase and the 0..1 microphone level and draws them. The level
// rides ONE inline CSS custom property, so every layer that reacts to the voice (halo,
// core, ring) reads the same number and the browser animates transform and colour off
// it; the alternative, a class per loudness band, would rewrite class lists ~16 times a
// second and still quantize the swell into steps.
//
// aria-hidden on purpose: the orb is decoration over a role="status" line that already
// announces the phase in words. A screen reader gains nothing from a circle.

const GLYPHS: Record<VoicePhase, typeof Ear> = {
  idle: Ear,
  listening: Ear,
  transcribing: Loader2,
  thinking: Sparkles,
  speaking: AudioLines,
  error: AlertTriangle,
};

export interface VoiceOrbProps {
  readonly phase: VoicePhase;
  /** 0..1 microphone level. Clamped here so a bad sample cannot blow up the layout. */
  readonly level: number;
}

export function VoiceOrb({ phase, level }: VoiceOrbProps) {
  const Glyph = GLYPHS[phase];
  const clamped = Number.isFinite(level) ? Math.min(1, Math.max(0, level)) : 0;
  return (
    <div
      className="voice-orb"
      data-phase={phase}
      data-testid="voice-orb"
      style={{ '--orb-level': clamped } as CSSProperties}
      aria-hidden="true"
    >
      <div className="voice-orb__core">
        <Glyph className="voice-orb__glyph size-8" focusable="false" />
      </div>
      <div className="voice-orb__ring" />
    </div>
  );
}
