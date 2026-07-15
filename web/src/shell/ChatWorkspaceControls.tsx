import { VoiceModeToggle } from '../chat/voice/VoiceModeToggle';
import { ArtifactsToggle } from './ArtifactsShell';

interface ChatWorkspaceControlsProps {
  readonly artifactsActive: boolean;
  readonly onArtifactsToggle: () => void;
}

export function ChatWorkspaceControls({
  artifactsActive,
  onArtifactsToggle,
}: ChatWorkspaceControlsProps) {
  return (
    <div
      data-chat-workspace-controls
      className="flex min-w-0 shrink-0 items-center justify-end gap-1 border-b border-border px-3 py-1.5"
    >
      <VoiceModeToggle />
      <ArtifactsToggle active={artifactsActive} onToggle={onArtifactsToggle} />
    </div>
  );
}
