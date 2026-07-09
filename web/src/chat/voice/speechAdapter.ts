import type { SpeechSynthesisAdapter } from '@assistant-ui/react';

// speechAdapter — a custom assistant-ui SpeechSynthesisAdapter that backs the
// per-message speaker control (D-03). `speak(text)` POSTs the text to /api/tts,
// turns the audio/mpeg blob into a `new Audio(objectURL)`, and plays it, exposing
// the {status, cancel, subscribe} Utterance the runtime's speak() drives.
//
// Three things beyond the stock WebSpeech adapter:
//  1. Per-text blob cache — a repeat Speak of the same message never re-bills
//     /api/tts (D-03, RESEARCH Landmine #5); the adapter is one thread-level
//     instance so the cache key is the text.
//  2. `truncated` — read from the X-Aura-TTS-Truncated response header and surfaced
//     both on the Utterance and stamped onto every emitted status object, so the
//     speaker control can render the D-05 "message too long" hint off s.message.speech
//     (the stock runtime copies utterance.status into speech by reference).
//  3. `dispose()` — revokes every cached object URL; 37C-05 invokes it on
//     chat/thread unmount so the blobs never leak (RESEARCH Landmine #5).

type Status = SpeechSynthesisAdapter.Status;
type TruncatedStatus = Status & { readonly truncated: boolean };

export interface SpeechUtterance extends SpeechSynthesisAdapter.Utterance {
  /** True when the backend capped the input text (X-Aura-TTS-Truncated) — drives the D-05 hint. */
  readonly truncated: boolean;
}

export interface SpeechAdapter extends SpeechSynthesisAdapter {
  speak: (text: string) => SpeechUtterance;
  /** Revoke every cached object URL. Invoked on chat/thread unmount by 37C-05 (Landmine #5). */
  dispose: () => void;
}

const TRUNCATED_HEADER = 'X-Aura-TTS-Truncated';

interface CacheEntry {
  readonly url: string;
  readonly truncated: boolean;
}

const runningStatus = (truncated: boolean): TruncatedStatus => ({ type: 'running', truncated });
const endedStatus = (
  reason: 'finished' | 'cancelled' | 'error',
  truncated: boolean,
): TruncatedStatus => ({ type: 'ended', reason, truncated });

export function createSpeechAdapter(): SpeechAdapter {
  // One thread-level instance ⇒ cache keyed on the message text (Landmine #5 / D-03).
  const cache = new Map<string, CacheEntry>();

  const speak = (text: string): SpeechUtterance => {
    const subscribers = new Set<() => void>();
    let audio: HTMLAudioElement | undefined;

    const utterance: {
      status: Status;
      truncated: boolean;
      cancel: () => void;
      subscribe: (callback: () => void) => () => void;
    } = {
      status: runningStatus(cache.get(text)?.truncated ?? false),
      truncated: cache.get(text)?.truncated ?? false,
      cancel: () => {
        audio?.pause();
        end('cancelled');
      },
      subscribe: (callback) => {
        subscribers.add(callback);
        return () => {
          subscribers.delete(callback);
        };
      },
    };

    const notify = (): void => {
      for (const callback of subscribers) callback();
    };

    function end(reason: 'finished' | 'cancelled' | 'error'): void {
      if (utterance.status.type === 'ended') return;
      utterance.status = endedStatus(reason, utterance.truncated);
      notify();
    }

    const play = (url: string, truncated: boolean): void => {
      utterance.truncated = truncated;
      utterance.status = runningStatus(truncated);
      audio = new Audio(url);
      audio.onended = () => {
        end('finished');
      };
      audio.onerror = () => {
        end('error');
      };
      void audio.play().catch(() => {
        end('error');
      });
    };

    const cached = cache.get(text);
    if (cached !== undefined) {
      play(cached.url, cached.truncated);
    } else {
      void (async () => {
        try {
          const res = await fetch('/api/tts', {
            method: 'POST',
            headers: { Accept: 'audio/mpeg', 'Content-Type': 'application/json' },
            credentials: 'same-origin',
            body: JSON.stringify({ text }),
          });
          if (!res.ok) {
            end('error');
            return;
          }
          const truncated = res.headers.get(TRUNCATED_HEADER) === 'true';
          const blob = await res.blob();
          const url = URL.createObjectURL(blob);
          cache.set(text, { url, truncated });
          if (utterance.status.type === 'ended') return; // cancelled before the blob arrived
          play(url, truncated);
        } catch {
          end('error');
        }
      })();
    }

    return utterance;
  };

  const dispose = (): void => {
    for (const { url } of cache.values()) URL.revokeObjectURL(url);
    cache.clear();
  };

  return { speak, dispose };
}
