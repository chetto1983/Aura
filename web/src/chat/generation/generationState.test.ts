import { describe, expect, it } from 'vitest';
import { REPLAYED_RESULT_MARKER, generationArgs, generationState } from './generationState';

// generationState / generationArgs — the pure gate between a media tool part and the
// generation frame. Only image_generate and video_generate are ever recognized, only a
// running part or a detached video job is drawn as a frame, and every argument the model
// streamed is untrusted text: a ratio outside the tool's enum never reaches CSS.

const DETACHED = '{"status":"in_progress","job_id":"job-1","message":"still running"}';

describe('generationState', () => {
  it('draws a running image or video call as a running frame', () => {
    expect(generationState('image_generate', 'running', undefined)).toBe('running');
    expect(generationState('video_generate', 'running', undefined)).toBe('running');
  });

  it('never recognizes another tool, whatever its status or result', () => {
    expect(generationState('send_file', 'running', undefined)).toBe('fallback');
    expect(generationState('web_search', 'complete', DETACHED)).toBe('fallback');
    expect(generationState('', 'running', DETACHED)).toBe('fallback');
  });

  it('a replayed detached video has a static frame', () => {
    expect(generationState('video_generate', 'complete', DETACHED)).toBe('deferred');
    expect(generationState('video_generate', undefined, DETACHED)).toBe('deferred');
  });

  it('a pending job collected before it started is still arriving', () => {
    expect(generationState('video_generate', 'complete', '{"status":"pending","job_id":"j"}')).toBe(
      'deferred',
    );
  });

  it('accepts a result that is already an object', () => {
    expect(generationState('video_generate', 'complete', { status: 'in_progress' })).toBe(
      'deferred',
    );
  });

  it('only a video job can be deferred', () => {
    expect(generationState('image_generate', 'complete', DETACHED)).toBe('fallback');
  });

  it('leaves errors, finished jobs and malformed results to the ordinary card', () => {
    const results: unknown[] = [
      '{"error":"job_failed","message":"The video generation job failed."}',
      '{"error":"content_blocked","message":"blocked"}',
      '{"status":"completed","job_id":"job-1"}',
      '{"status":"in_progress"',
      'null',
      '"in_progress"',
      '[{"status":"in_progress"}]',
      42,
      null,
      undefined,
    ];
    for (const result of results) {
      expect(generationState('video_generate', 'complete', result)).toBe('fallback');
    }
  });

  it('strips the gateway replay marker before parsing the result', () => {
    const replayed = `${DETACHED}${REPLAYED_RESULT_MARKER}`;
    expect(generationState('video_generate', 'complete', replayed)).toBe('deferred');
  });

  it('pins the exact suffix internal/gateway/reserve.go appends', () => {
    expect(REPLAYED_RESULT_MARKER).toBe(
      '\n\n[replayed: this result is from a prior dispatch of this call, not a fresh execution]',
    );
  });

  it('does not strip the marker text when it is not the suffix', () => {
    const embedded = `${DETACHED}${REPLAYED_RESULT_MARKER} trailing`;
    expect(generationState('video_generate', 'complete', embedded)).toBe('fallback');
    const leading = `${REPLAYED_RESULT_MARKER}${DETACHED}`;
    expect(generationState('video_generate', 'complete', leading)).toBe('fallback');
  });
});

describe('generationArgs', () => {
  it('rejects executable or malformed ratio text', () => {
    expect(
      generationArgs('{"prompt":"<img onerror=alert(1)>","aspect_ratio":"url(evil)"}'),
    ).toEqual({ prompt: '<img onerror=alert(1)>', aspectRatio: '1 / 1' });
  });

  it('maps every ratio the two tools enumerate to a CSS aspect-ratio value', () => {
    const ratios: readonly (readonly [string, string])[] = [
      ['1:1', '1 / 1'],
      ['16:9', '16 / 9'],
      ['9:16', '9 / 16'],
      ['4:3', '4 / 3'],
      ['3:4', '3 / 4'],
      ['3:2', '3 / 2'],
      ['2:3', '2 / 3'],
      ['21:9', '21 / 9'],
      ['9:21', '9 / 21'],
    ];
    for (const [ratio, css] of ratios) {
      expect(generationArgs(JSON.stringify({ prompt: 'p', aspect_ratio: ratio }))).toEqual({
        prompt: 'p',
        aspectRatio: css,
      });
    }
  });

  it('never resolves an inherited object key as a ratio', () => {
    for (const ratio of ['constructor', '__proto__', 'toString', '16 / 9', ' 16:9']) {
      expect(generationArgs(JSON.stringify({ prompt: 'p', aspect_ratio: ratio })).aspectRatio).toBe(
        '1 / 1',
      );
    }
  });

  it('treats partial, missing or non-object arguments as an empty square frame', () => {
    const empty = { prompt: '', aspectRatio: '1 / 1' };
    expect(generationArgs(undefined)).toEqual(empty);
    expect(generationArgs('')).toEqual(empty);
    expect(generationArgs('{"prompt":"A moving s')).toEqual(empty);
    expect(generationArgs('["prompt"]')).toEqual(empty);
    expect(generationArgs('null')).toEqual(empty);
    expect(generationArgs('{"prompt":42,"aspect_ratio":16}')).toEqual(empty);
  });

  it('keeps the prompt when only the ratio is absent', () => {
    expect(generationArgs('{"prompt":"A calm lake"}')).toEqual({
      prompt: 'A calm lake',
      aspectRatio: '1 / 1',
    });
  });
});
