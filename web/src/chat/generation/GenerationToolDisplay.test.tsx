import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { CollectedJobsContext } from './collectedJobsContext';
import { REPLAYED_RESULT_MARKER } from './generationState';
import { GenerationToolDisplay } from './GenerationToolDisplay';

// GenerationToolDisplay: presentation only. It turns a media tool part into the frame for
// the two states the frame owns and renders nothing otherwise; ToolFallback keeps every
// other outcome (errors included) on the ordinary tool card.

afterEach(() => {
  vi.useRealTimers();
});

const ARGS = '{"prompt":"A moving sea","aspect_ratio":"16:9"}';
const DETACHED = '{"status":"in_progress","job_id":"job-1"}';

function visualRatio(): string {
  const node = document.querySelector<HTMLElement>('[data-slot="generation-visual"]');
  if (node === null) throw new Error('expected the frame visual');
  return node.style.aspectRatio;
}

describe('GenerationToolDisplay', () => {
  it('draws a running image call with its prompt, ratio and counter', () => {
    vi.useFakeTimers();
    render(
      <GenerationToolDisplay
        toolName="image_generate"
        argsText='{"prompt":"A calm lake","aspect_ratio":"3:2"}'
        statusType="running"
        result={undefined}
      />,
    );
    expect(screen.getByTestId('generation-frame').getAttribute('data-generating')).toBe('true');
    expect(screen.getByText('Generating image')).toBeTruthy();
    expect(screen.getByText('A calm lake')).toBeTruthy();
    expect(screen.getByRole('timer').textContent).toBe('0:00');
    expect(visualRatio()).toBe('3 / 2');
  });

  it('draws a running video call as a video frame', () => {
    render(
      <GenerationToolDisplay
        toolName="video_generate"
        argsText={ARGS}
        statusType="running"
        result={undefined}
      />,
    );
    expect(screen.getByText('Generating video')).toBeTruthy();
    expect(visualRatio()).toBe('16 / 9');
  });

  it('draws a detached video job as the static arriving frame', () => {
    render(
      <GenerationToolDisplay
        toolName="video_generate"
        argsText={ARGS}
        statusType="complete"
        result={`${DETACHED}${REPLAYED_RESULT_MARKER}`}
      />,
    );
    expect(screen.getByTestId('generation-frame').getAttribute('data-generating')).toBe('false');
    expect(screen.getByText('Arriving in this chat')).toBeTruthy();
    expect(screen.queryByRole('timer')).toBeNull();
  });

  it('renders nothing for an outcome the frame does not own', () => {
    const { container } = render(
      <GenerationToolDisplay
        toolName="video_generate"
        argsText={ARGS}
        statusType="complete"
        result='{"error":"job_failed","message":"failed"}'
      />,
    );
    expect(container.innerHTML).toBe('');
  });
});

describe('a detached job this thread already collected', () => {
  it('draws nothing, because its clip is already on screen', () => {
    render(
      <CollectedJobsContext.Provider value={new Set(['job-1'])}>
        <GenerationToolDisplay
          toolName="video_generate"
          argsText={ARGS}
          statusType="complete"
          result={DETACHED}
        />
      </CollectedJobsContext.Provider>,
    );
    expect(screen.queryByTestId('generation-frame')).toBeNull();
  });

  it('still draws a detached job nobody has collected', () => {
    render(
      <CollectedJobsContext.Provider value={new Set(['job-other'])}>
        <GenerationToolDisplay
          toolName="video_generate"
          argsText={ARGS}
          statusType="complete"
          result={DETACHED}
        />
      </CollectedJobsContext.Provider>,
    );
    expect(screen.getByTestId('generation-frame').getAttribute('data-generating')).toBe('false');
  });

  it('keeps a RUNNING call visible even when its job id was collected', () => {
    vi.useFakeTimers();
    render(
      <CollectedJobsContext.Provider value={new Set(['job-1'])}>
        <GenerationToolDisplay
          toolName="video_generate"
          argsText={ARGS}
          statusType="running"
          result={undefined}
        />
      </CollectedJobsContext.Provider>,
    );
    expect(screen.getByTestId('generation-frame').getAttribute('data-generating')).toBe('true');
  });
});
