import { VoiceModeToggle } from '../chat/voice/VoiceModeToggle';
import { ArtifactsToggle } from './ArtifactsShell';
import { ShareToggle } from './ShareShell';

interface ChatWorkspaceControlsProps {
  readonly artifactsActive: boolean;
  readonly onArtifactsToggle: () => void;
  readonly onShareOpen: () => void;
}

export function ChatWorkspaceControls({
  artifactsActive,
  onArtifactsToggle,
  onShareOpen,
}: ChatWorkspaceControlsProps) {
  return (
    <div
      data-chat-workspace-controls
      className="flex min-w-0 shrink-0 items-center justify-end gap-1 border-b border-border px-3 py-1.5"
    >
      <VoiceModeToggle />
      {/* 37F plan 14: hasInternalShare/hasPublicShare are hardcoded false — no share-list data
          hook exists yet. Plan 37F-15 (which mounts the actual ShareModal in AppShell.tsx) wires
          the real query alongside it. Tracked as a Known Stub in 37F-14-SUMMARY.md. */}
      <ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={onShareOpen} />
      <ArtifactsToggle active={artifactsActive} onToggle={onArtifactsToggle} />
    </div>
  );
}
