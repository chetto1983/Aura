import { useEffect, useRef } from 'react';
import { useAui, useAuiState } from '@assistant-ui/react';
import { useVoiceMode } from './voiceModeContext';
import { shouldSpeak } from './shouldSpeak';

// useAutoSpeak — auto-speaks a newly-completed assistant reply when
// shouldSpeak(voiceMode, turnWasDictated) is true (D-07). Mounted inside the runtime
// subtree by 37C-05. It tracks the last completed assistant message id and, when a
// genuinely NEW one settles (the turn is no longer running), calls the runtime speak
// once, then consumes turnWasDictated so a subsequent typed turn does not auto-speak.

interface AutoSpeakMessage {
  readonly id: string;
  readonly role: string;
}

/** The id of the last assistant message once the turn has settled (undefined while running). */
function latestCompletedAssistantId(
  messages: readonly AutoSpeakMessage[],
  isRunning: boolean,
): string | undefined {
  if (isRunning) return undefined;
  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i];
    if (message?.role === 'assistant') return message.id;
  }
  return undefined;
}

export function useAutoSpeak(): void {
  const aui = useAui();
  const { voiceMode, turnWasDictated, clearTurnDictated } = useVoiceMode();
  const messages = useAuiState((s) => s.thread.messages);
  const isRunning = useAuiState((s) => s.thread.isRunning);

  const lastSpokenRef = useRef<string | undefined>(undefined);
  const initializedRef = useRef(false);

  const latestId = latestCompletedAssistantId(messages, isRunning);

  useEffect(() => {
    // On first mount adopt whatever reply is already on screen (rehydrated history)
    // WITHOUT speaking it — only replies that arrive AFTER mount auto-speak.
    if (!initializedRef.current) {
      initializedRef.current = true;
      lastSpokenRef.current = latestId;
      return;
    }
    if (latestId === undefined || latestId === lastSpokenRef.current) return;
    lastSpokenRef.current = latestId;
    if (shouldSpeak(voiceMode, turnWasDictated)) {
      // assistant-ui 0.15 marks the whole speech API "under active development"; there is
      // no stable replacement for per-message speak, and dropping it would silently kill
      // voice mode's read-back. Pinned deliberately, not ignored wholesale.
      // eslint-disable-next-line @typescript-eslint/no-deprecated
      aui.thread.message({ id: latestId }).speak();
    }
    // Consume the dictation flag so the next (typed) turn does not auto-speak.
    clearTurnDictated();
  }, [latestId, voiceMode, turnWasDictated, aui, clearTurnDictated]);
}
