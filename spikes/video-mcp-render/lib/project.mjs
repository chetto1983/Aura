// The parity project: every Studio feature the sidecar must reproduce, laid out so each one owns a
// stretch of the film where its effect can be measured alone. 60 s at 30 fps.
//
//   video   A 0–20.5 (fade-in) ╳ B 19.5–40 ╳ C 39–60 (fade-out); 1 s crossfades at 19.5 and 39
//   text    on B, 21.5–29.5, the cockpit's own Atkinson Hyperlegible Next
//   sound   A's own tone 0–5 s (source audio stops at 5 s); B muted; C's tone 55–60 at volume 0.5
//   music   5–55, volume 0.8, fade-in 2 s, fade-out 3 s, envelope 1 → 0.5 over 30–35 s,
//           ducking −12 dB with 0.5 s ramps under the voice
//   voice   speech.wav from 8 s at volume 0.01 (−40 dB), as the ducking E2E does, so every level
//           measured in its windows is the music's; its speech windows are the fixture's truth
export const SPEECH_TRUTH = [
  [1, 3.662],
  [5.162, 7.857],
  [9.357, 11.905],
];
export const VOICE_AT = 8;
export const SOURCE_SECONDS = 21;

export function parityProject({ width = 1920, height = 1080 } = {}) {
  const video = (id, assetId) => ({
    id,
    assetId,
    kind: 'video',
    duration: SOURCE_SECONDS,
    size: { width: 1920, height: 1080 },
    hasAudio: true,
  });
  const crossfadeFrom = (clipId) => ({ junctionFromClipId: clipId, junctionTransition: 'crossfade', junctionDuration: 1 });
  return {
    id: 'parity-project',
    name: 'parity',
    size: { width, height },
    fps: 30,
    sources: [
      video('src-a', 'clip-a.mp4'),
      video('src-b', 'clip-b.mp4'),
      video('src-c', 'clip-c.mp4'),
      { id: 'src-music', assetId: 'music-bed.wav', kind: 'audio', duration: 50, size: { width: 0, height: 0 } },
      {
        id: 'src-voice',
        assetId: 'speech.wav',
        kind: 'audio',
        duration: 12.9,
        size: { width: 0, height: 0 },
        speech: SPEECH_TRUTH,
      },
    ],
    video: [
      { id: 'clip-a', sourceId: 'src-a', duration: 20.5, sourceStart: 0, muted: false, fadeIn: true },
      { id: 'clip-b', sourceId: 'src-b', duration: 20.5, sourceStart: 0, muted: true, ...crossfadeFrom('clip-a') },
      { id: 'clip-c', sourceId: 'src-c', duration: 21, sourceStart: 0, muted: false, volume: 0.5, fadeOut: true, ...crossfadeFrom('clip-b') },
    ],
    overlays: [
      {
        id: 'titles',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-b', offset: 2 },
            duration: 8,
            props: { text: 'Aura parity — 1080p', fontFamily: 'Atkinson Hyperlegible Next', fontSize: 5, fontWeight: 700, color: '#ffffff' },
          },
        ],
      },
    ],
    audio: [
      {
        id: 'music-lane',
        items: [
          {
            id: 'music',
            sourceId: 'src-music',
            anchor: { clipId: 'clip-a', offset: 5 },
            sourceStart: 0,
            duration: 50,
            volume: 0.8,
            muted: false,
            fadeIn: 2,
            fadeOut: 3,
            envelope: [
              { time: 0, gain: 1 },
              { time: 25, gain: 1 },
              { time: 30, gain: 0.5 },
              { time: 50, gain: 0.5 },
            ],
            ducking: { amountDb: -12, ramp: 0.5 },
          },
        ],
      },
      {
        id: 'voice-lane',
        items: [
          {
            id: 'voice',
            sourceId: 'src-voice',
            anchor: { clipId: 'clip-a', offset: VOICE_AT },
            sourceStart: 0,
            duration: 12.9,
            volume: 0.01,
            muted: false,
          },
        ],
      },
    ],
  };
}

/** The windows each measurement reads, in film seconds. */
export const WINDOWS = {
  clipTone: [1, 4],
  musicUp: [[7.2, 8.4], [20.6, 29.5]],
  musicDucked: SPEECH_TRUTH.map(([s, e]) => [VOICE_AT + s + 0.5, VOICE_AT + e - 0.5]),
  musicEnvelopeHalf: [36, 51],
  clipCTone: [56, 59],
};

/** Frames compared across renders: mid-crossfade, the title, and C's fade-out. */
export const FRAMES = { crossfade: 20.0, title: 25.0, fadeOut: 59.8 };
