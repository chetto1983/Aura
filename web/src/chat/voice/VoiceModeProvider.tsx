import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { VoiceModeContext, type VoiceModeState } from './voiceModeContext';
import { useVoiceCapabilities } from './useVoiceCapabilities';

// VoiceModeProvider — ephemeral, session-scoped voice-mode state (D-06). NO
// persistence: no browser storage, no server preference (Telegram's VoiceModePref stays
// a false stub). The toggle resets on reload. `turnWasDictated` is set by 37C-05's
// dictation adapter (markTurnDictated) and consumed by useAutoSpeak (clearTurnDictated)
// so a dictated turn auto-speaks its reply without persisting anything.
export function VoiceModeProvider({ children }: { readonly children: ReactNode }) {
  const caps = useVoiceCapabilities();
  const [voiceMode, setVoiceMode] = useState(false);
  const [turnWasDictated, setTurnWasDictated] = useState(false);

  const toggleVoiceMode = useCallback(() => {
    setVoiceMode((on) => !on);
  }, []);
  const markTurnDictated = useCallback(() => {
    setTurnWasDictated(true);
  }, []);
  const clearTurnDictated = useCallback(() => {
    setTurnWasDictated(false);
  }, []);

  const value = useMemo<VoiceModeState>(
    () => ({
      caps,
      voiceMode,
      turnWasDictated,
      toggleVoiceMode,
      markTurnDictated,
      clearTurnDictated,
    }),
    [caps, voiceMode, turnWasDictated, toggleVoiceMode, markTurnDictated, clearTurnDictated],
  );

  return <VoiceModeContext.Provider value={value}>{children}</VoiceModeContext.Provider>;
}
