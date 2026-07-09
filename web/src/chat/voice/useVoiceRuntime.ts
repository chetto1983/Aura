import { useEffect, useMemo } from 'react';
import type { DictationAdapter, SpeechSynthesisAdapter } from '@assistant-ui/react';
import { createDictationAdapter } from './dictationAdapter';
import { createSpeechAdapter } from './speechAdapter';
import { useVoiceMode } from './voiceModeContext';

// The adapters object handed to useExternalStoreRuntime. A missing key makes the runtime
// report that capability false — the native degrade (RESEARCH Q1: capabilities derive from
// adapter presence; no RuntimeAdapterProvider on the external-store path).
export interface VoiceRuntimeAdapters {
  readonly speech?: SpeechSynthesisAdapter;
  readonly dictation?: DictationAdapter;
}

// useVoiceRuntime builds the thread-scoped speech + dictation adapters ONCE, gates each on
// the probed capability, and revokes the speech adapter's cached object URLs when the
// chat/runtime subtree unmounts (RESEARCH Landmine #5 — per-message unmount is unreachable
// from a thread-scoped adapter, so teardown-revoke on unmount is the blob-URL leak fix).
// Extracted from ExternalStoreChat.tsx so that near-cap file stays ≤600 LOC.
export function useVoiceRuntime(): VoiceRuntimeAdapters {
  const { caps } = useVoiceMode();
  const speechAdapter = useMemo(() => createSpeechAdapter(), []);
  const dictationAdapter = useMemo(() => createDictationAdapter(), []);

  useEffect(() => {
    return () => {
      speechAdapter.dispose();
    };
  }, [speechAdapter]);

  return {
    ...(caps.tts ? { speech: speechAdapter } : {}),
    ...(caps.stt ? { dictation: dictationAdapter } : {}),
  };
}
