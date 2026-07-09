import { useEffect, useState } from 'react';

// useVoiceCapabilities — the one-shot probe of GET /api/voice/capabilities (D-11).
// The SPA reads it once on load to decide whether the speaker control + voice-mode
// toggle (tts) and the mic dictation adapter (stt) are wired at all. The endpoint
// answers 200 `{false,false}` even when unconfigured, so any error or non-2xx keeps
// the safe default — the controls simply stay hidden (WEBVOICE-03 graceful degrade).
// Fetch shape mirrors settingsApi.fetchSettings (Accept json + same-origin cookie).

export interface VoiceCapabilities {
  readonly tts: boolean;
  readonly stt: boolean;
}

const DISABLED: VoiceCapabilities = { tts: false, stt: false };

export function useVoiceCapabilities(): VoiceCapabilities {
  const [caps, setCaps] = useState<VoiceCapabilities>(DISABLED);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const res = await fetch('/api/voice/capabilities', {
          headers: { Accept: 'application/json' },
          credentials: 'same-origin',
          signal: controller.signal,
        });
        if (!res.ok) return;
        const raw = (await res.json()) as { tts?: unknown; stt?: unknown };
        setCaps({ tts: raw.tts === true, stt: raw.stt === true });
      } catch {
        // Aborted on unmount, or a network/parse failure → keep the disabled
        // default; the voice routes simply stay hidden (WEBVOICE-03 degrade).
      }
    })();
    return () => {
      controller.abort();
    };
  }, []);

  return caps;
}
