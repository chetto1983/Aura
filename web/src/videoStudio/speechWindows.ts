// speechWindows.ts — the VAD's 30 ms verdicts as speech windows, smoothed as S4 measured them
// (spikes/video-studio-audio/FINDINGS.md): a pause shorter than 300 ms joins its neighbours, a
// window shorter than 250 ms is dropped. Pure, so the smoothing is tested without the WASM that
// produces the verdicts.

export type SpeechWindow = readonly [number, number];

/** A pause shorter than this is a breath inside one phrase. */
const MERGE_GAP = 0.3;
/** A window shorter than this is a click, not speech. */
const MIN_SPEECH = 0.25;
/** Ten frames of 0.03 s land a hair under 0.3 s in floating point: this keeps them on the side the
 *  spike's integer-millisecond comparison put them. */
const EPSILON = 1e-9;

/** The voiced frames as smoothed windows, in seconds from the first frame. */
export function speechFromFrames(voiced: readonly boolean[], frameSeconds: number): SpeechWindow[] {
  const merged: [number, number][] = [];
  voiced.forEach((isVoiced, index) => {
    if (!isVoiced) return;
    const start = index * frameSeconds;
    const last = merged.at(-1);
    if (last !== undefined && start - last[1] < MERGE_GAP - EPSILON) last[1] = start + frameSeconds;
    else merged.push([start, start + frameSeconds]);
  });
  return merged.filter(([start, end]) => end - start >= MIN_SPEECH - EPSILON);
}
