import { GenerationFrame } from './GenerationFrame';
import { generationArgs, generationState } from './generationState';

// GenerationToolDisplay — presentation only: the frame for a media tool part that is running
// or detached, nothing otherwise. ToolFallback decides when to mount it and keeps every other
// outcome, errors included, on the ordinary tool card.

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
  const state = generationState(toolName, statusType, result);
  if (state === 'fallback') return null;
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
