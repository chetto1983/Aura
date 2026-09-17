import { useCollectedJobs } from './collectedJobsContext';
import { GenerationFrame } from './GenerationFrame';
import { deferredJobId, generationArgs, generationState } from './generationState';

// GenerationToolDisplay — presentation only: the frame for a media tool part that is running
// or detached, nothing otherwise. ToolFallback decides when to mount it and keeps every other
// outcome, errors included, on the ordinary tool card.
//
// A detached job's frame is dropped once this thread has collected that job: the clip is
// already on screen further down, and the frame above it would be a second, empty copy of the
// same generation (seen in the cockpit on 2026-09-17).

export interface GenerationToolDisplayProps {
  readonly toolName: string;
  readonly argsText: string | undefined;
  readonly statusType: string | undefined;
  readonly result: unknown;
}

export function GenerationToolDisplay({
  toolName,
  argsText,
  statusType,
  result,
}: GenerationToolDisplayProps) {
  const collected = useCollectedJobs();
  const state = generationState(toolName, statusType, result);
  if (state === 'fallback') return null;
  if (state === 'deferred') {
    const jobID = deferredJobId(result);
    if (jobID !== undefined && collected.has(jobID)) return null;
  }
  const { prompt, aspectRatio } = generationArgs(argsText);
  return (
    <GenerationFrame
      kind={toolName === 'video_generate' ? 'video' : 'image'}
      prompt={prompt}
      aspectRatio={aspectRatio}
      generating={state === 'running'}
    />
  );
}
