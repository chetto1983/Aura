import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

const mutationTests = [
  'src/approvals/__tests__/ApprovalList.test.tsx',
  'src/approvals/__tests__/InlineApprovalCard.test.tsx',
  'src/approvals/__tests__/ThreadApprovalCards.test.tsx',
  'src/approvals/__tests__/approvalState.test.ts',
  // The question frame (spec 2026-09-25): the ask_user adapter's suites above reach it too.
  'src/questions/__tests__/QuestionFrame.test.tsx',
  // A mounted server's form (spec 2026-09-25): the pump signal, the thread's fold, the route
  // client, the step logic, the countdown and the card.
  'src/chat/sseAdapter.onElicitation.test.ts',
  'src/questions/__tests__/useThreadElicitations.test.ts',
  'src/questions/__tests__/elicitationApi.test.ts',
  'src/questions/__tests__/elicitationSteps.test.ts',
  'src/questions/__tests__/useCountdown.test.ts',
  'src/questions/__tests__/ElicitationCard.test.tsx',
  'src/chat/artifacts/artifactMeta.test.ts',
  'src/chat/artifacts/downloadAll.test.ts',
  // iOS's home-screen app saves through the share sheet (prd.md §3).
  'src/chat/artifacts/SaveFileLink.test.tsx',
  'src/lib/__tests__/installedApp.test.ts',
  'src/chat/voice/speechAdapter.test.ts',
  'src/chat/voice/dictationAdapter.test.ts',
  'src/chat/displays/__tests__/SourcesButton.test.tsx',
  'src/chat/displays/__tests__/sourceExplorerData.test.ts',
  'src/chat/displays/__tests__/swarmRow.test.ts',
  'src/chat/displays/__tests__/tableData.test.ts',
  'src/governance/__tests__/BoardStateView.test.tsx',
  'src/governance/__tests__/governanceApi.test.ts',
  'src/governance/__tests__/helpers.test.ts',
  'src/graph/__tests__/ArcadeGraphCanvas_data.test.ts',
  'src/graph/__tests__/graphIntent.test.ts',
  'src/onboarding/__tests__/onboardingApi.test.ts',
  'src/onboarding/__tests__/onboardingWizardModel.test.ts',
  'src/chat/share/shareApi.test.ts',
  'src/chat/share/SharedSection.test.tsx',
  'src/chat/share/ShareModal.test.tsx',
  'src/chat/share/shareViewModel.test.ts',
  'src/shell/ShareShell.test.tsx',
  // Compact-chat + audit modules (2026-07-23): pure logic under mutation.
  // ReasoningPill.test rides along as the durationFormat consumer suite.
  'src/chat/__tests__/toolSummary.test.ts',
  'src/chat/__tests__/ToolActivityCard.test.tsx',
  'src/chat/__tests__/ToolGroup.test.tsx',
  'src/chat/__tests__/toolGrouping.test.ts',
  'src/chat/__tests__/durationFormat.test.ts',
  'src/chat/__tests__/ReasoningPill.test.tsx',
  'src/audit/__tests__/auditPairing.test.ts',
  'src/conversations/__tests__/exportConversation.test.ts',
  'src/conversations/__tests__/useConversationTitle.test.ts',
  'src/chat/__tests__/snapshotToolOutcome.test.ts',
  'src/chat/__tests__/sseAdapter_snapshot.test.ts',
  'src/chat/__tests__/sseAdapter_network.test.ts',
  'src/chat/ExternalStoreChat.reasoning.test.tsx',
  'src/chat/displays/__tests__/snapshotToMessages.test.ts',
  // Generation cockpit (2026-09-16): the owned registry image/image-generation
  // customizations, the generation frame/state/tool display, and the two media
  // renderers plus the dispatch switch they are reached through. Scored on their own
  // denominator by critical_mutation_gate's media_frontend scope, so a survivor here
  // cannot be averaged away by the suites above.
  'src/chat/generation/GenerationFrame.test.tsx',
  'src/chat/generation/GenerationToolDisplay.test.tsx',
  'src/chat/generation/generationState.test.ts',
  'src/browserLive/liveInput.test.ts',
  'src/chat/browser/liveBrowserSession.test.ts',
  'src/components/__tests__/computer-use.test.tsx',
  'src/routes/BrowserLivePage.test.tsx',
  'src/chat/generation/generationThread.test.tsx',
  'src/chat/artifacts/renderers/GeneratedImagePreview.test.tsx',
  'src/chat/artifacts/renderers/VideoPreview.test.tsx',
  'src/chat/artifacts/PreviewModal.test.tsx',
  // stryker.config.json mutates these modules, so their suites must run here too: missing
  // from this list, all 313 of their mutants scored NoCoverage and pulled the whole run to
  // 69.37% (CI 2026-09-24) although every one of them has a test.
  'src/settings/__tests__/embeddingBackendState.test.ts',
  'src/update/__tests__/systemUpdateApi.test.ts',
  'src/update/__tests__/updateModel.test.ts',
  'src/update/__tests__/updateTime.test.ts',
  'src/update/__tests__/useSystemUpdate.test.tsx',
  // Video Studio audio core (spec 2026-09-27): the lane rules, the audio commands, the volume
  // curve and the audio compile.
  'src/videoStudio/__tests__/audioLane.test.ts',
  'src/videoStudio/__tests__/commands_audio.test.ts',
  'src/videoStudio/__tests__/volumeCurve.test.ts',
  'src/videoStudio/__tests__/sourceClock.test.ts',
  'src/videoStudio/__tests__/videoflow_audio.test.ts',
  // aura-video-mcp Plan A: the fade compile, read through the renderer's own runtime layers.
  'src/videoStudio/__tests__/videoflow_keyframes.test.ts',
  // aura-video-mcp Plan A: an export's media, once per source, and the sound-only export.
  'src/videoStudio/__tests__/videoflow_export.test.ts',
  'src/videoStudio/__tests__/videoflow_exportAudio.test.ts',
  // Conversation search: the snippet that shows where a word matched (prd.md §7).
  'src/conversations/__tests__/SearchPanel.test.tsx',
] as const;

export default defineConfig({
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: { fs: { allow: ['..'] } },
  test: {
    environment: 'jsdom',
    globals: true,
    include: [...mutationTests],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    setupFiles: ['./src/test/setup.ts'],
  },
});
