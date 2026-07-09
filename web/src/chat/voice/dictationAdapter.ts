import type { DictationAdapter } from '@assistant-ui/react';

// dictationAdapter — a custom assistant-ui DictationAdapter that backs the Composer's
// in-place dictation (D-09). `listen()` opens the mic via getUserMedia, records with a
// MediaRecorder, and on stop POSTs the blob to /api/stt (transcribe-and-discard, D-08).
//
// The critical contract (RESEARCH Landmine #1): the composer inserts the transcript
// through `onSpeech({transcript, isFinal:true})` — NOT `onSpeechEnd`, whose payload the
// core discards (base-composer-runtime-core.ts:474-476 only tears the session down). So
// on a 200 we fire `onSpeech` FIRST (the insert), then `onSpeechEnd` (cleanup). An empty
// transcript inserts nothing and still ends clean (reason:stopped). A non-ok /api/stt
// ends the session reason:error with NO onSpeech, so the Composer degrades (D-10).
//
// `stop()` resolves only AFTER the POST + callbacks complete: the composer runs
// `session.stop().finally(cleanup)`, and that cleanup unsubscribes onSpeech — so if stop()
// resolved before the POST, the insert would be dropped. Awaiting the recorder's onstop
// keeps the composer's dictation state alive across the transcribe window.

type Result = DictationAdapter.Result;
type ResultCallback = (result: Result) => void;

const STT_ROUTE = '/api/stt';

export function createDictationAdapter(): DictationAdapter {
  return {
    // A brief record→transcribe window with no interim stream: disable the input while
    // dictating, then re-enable it with the inserted (editable) transcript.
    disableInputDuringDictation: true,
    listen(): DictationAdapter.Session {
      const startCbs = new Set<() => void>();
      const endCbs = new Set<ResultCallback>();
      const speechCbs = new Set<ResultCallback>();

      const chunks: Blob[] = [];
      let recorder: MediaRecorder | undefined;
      let stream: MediaStream | undefined;
      // A holder object (not a bare `let`) so reads survive the flow-narrowing that would
      // otherwise flag the cross-closure mutation from cancel() as an impossible branch.
      const flags = { cancelled: false };

      // stop() awaits this so the composer's post-stop cleanup runs only after the POST +
      // onSpeech insert have completed (see the header note on the ordering trap).
      let settle: (() => void) | undefined;
      const settled = new Promise<void>((resolve) => {
        settle = resolve;
      });

      const stopTracks = (): void => {
        for (const track of stream?.getTracks() ?? []) track.stop();
      };

      const session: DictationAdapter.Session = {
        status: { type: 'starting' },
        stop: async () => {
          recorder?.stop();
          await settled;
        },
        cancel: () => {
          flags.cancelled = true;
          recorder?.stop();
          stopTracks();
          session.status = { type: 'ended', reason: 'cancelled' };
          settle?.();
        },
        onSpeechStart: (cb) => {
          startCbs.add(cb);
          return () => startCbs.delete(cb);
        },
        onSpeechEnd: (cb) => {
          endCbs.add(cb);
          return () => endCbs.delete(cb);
        },
        onSpeech: (cb) => {
          speechCbs.add(cb);
          return () => speechCbs.delete(cb);
        },
      };

      const transcribe = async (): Promise<void> => {
        stopTracks();
        if (flags.cancelled) return; // cancel() already ended the session; never POST
        try {
          const blob = new Blob(chunks, { type: recorder?.mimeType ?? 'audio/webm' });
          const form = new FormData();
          form.append('audio', blob, 'dictation');
          const res = await fetch(STT_ROUTE, {
            method: 'POST',
            credentials: 'same-origin',
            body: form,
          });
          if (!res.ok) {
            session.status = { type: 'ended', reason: 'error' }; // Composer degrades (D-10)
            return;
          }
          const data = (await res.json()) as { text?: unknown };
          const transcript = typeof data.text === 'string' ? data.text : '';
          // Insert via onSpeech (isFinal) — the ONLY path the core writes into the composer.
          if (transcript.length > 0) {
            for (const cb of speechCbs) cb({ transcript, isFinal: true });
          }
          session.status = { type: 'ended', reason: 'stopped' };
          for (const cb of endCbs) cb({ transcript }); // cleanup (payload ignored by core)
        } catch {
          session.status = { type: 'ended', reason: 'error' };
        }
      };

      void (async () => {
        try {
          stream = await navigator.mediaDevices.getUserMedia({ audio: true });
          if (flags.cancelled) {
            stopTracks();
            return;
          }
          recorder = new MediaRecorder(stream);
          recorder.ondataavailable = (event) => {
            if (event.data.size > 0) chunks.push(event.data);
          };
          recorder.onstop = () => {
            void transcribe().finally(() => settle?.());
          };
          recorder.start();
          session.status = { type: 'running' };
          for (const cb of startCbs) cb();
        } catch {
          session.status = { type: 'ended', reason: 'error' };
          settle?.();
        }
      })();

      return session;
    },
  };
}
