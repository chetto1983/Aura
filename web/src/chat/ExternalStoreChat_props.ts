import type { ComposerDraftPrompt } from './Composer';
import type { RunUsageEvent } from './runUsage';
import type { RunSessionBaselineListener } from './useRunUsageBaseline';

export interface ExternalStoreChatProps {
  /** Conversation/thread id the run is POSTed against. */
  readonly threadId: string;
  /** Create/select a conversation before the first send when no thread is active. */
  readonly onEnsureThread?: (initialPrompt: string) => Promise<string>;
  readonly onUsage?: (event: RunUsageEvent) => void;
  readonly onUsageBaseline?: RunSessionBaselineListener;
  readonly allocateUsageRunId?: () => number;
  /**
   * 37B seam (mirrors onUsage): fires when a run emits an `aura.artifact` descriptor,
   * carrying its asset_id. AppShell invalidates ['assets', threadId] + drives the
   * one-time Artefatti panel auto-open (D-11). Forwarded into streamRun/streamPost.
   */
  readonly onArtifact?: (assetId: string | undefined) => void;
  readonly draftPrompt?: ComposerDraftPrompt | undefined;
  readonly onDraftPromptConsumed?: (nonce: number) => void;
  readonly onRequestDraftPrompt?: (text: string) => void;
  /** 37D: threads AppShell's startNewConversation to the composer's new-chat quick action. */
}
