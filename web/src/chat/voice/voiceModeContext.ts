import { createContext, useContext } from 'react';
import type { VoiceCapabilities } from './useVoiceCapabilities';

// voiceModeContext — the context object + consumer hook for the ephemeral voice
// mode (D-06). Kept in a NON-component module (mirroring sourceExplorerControls.ts)
// so VoiceModeProvider.tsx stays component-only (react-refresh/only-export-components).
// useVoiceMode returns a DISABLED default when no provider is mounted, so the speaker
// control degrades to "absent" outside the provider (e.g. the isolated chat tests)
// rather than throwing.

export interface VoiceModeState {
  readonly caps: VoiceCapabilities;
  readonly voiceMode: boolean;
  readonly turnWasDictated: boolean;
  readonly toggleVoiceMode: () => void;
  readonly markTurnDictated: () => void;
  readonly clearTurnDictated: () => void;
}

const DISABLED: VoiceModeState = {
  caps: { tts: false, stt: false },
  voiceMode: false,
  turnWasDictated: false,
  toggleVoiceMode: () => undefined,
  markTurnDictated: () => undefined,
  clearTurnDictated: () => undefined,
};

export const VoiceModeContext = createContext<VoiceModeState>(DISABLED);

/** Read the voice-mode state; a disabled default (caps false) when no provider is mounted. */
export function useVoiceMode(): VoiceModeState {
  return useContext(VoiceModeContext);
}
