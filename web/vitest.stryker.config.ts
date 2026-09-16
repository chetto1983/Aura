import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

const mutationTests = [
  'src/approvals/__tests__/ApprovalList.test.tsx',
  'src/approvals/__tests__/InlineApprovalCard.test.tsx',
  'src/approvals/__tests__/ThreadApprovalCards.test.tsx',
  'src/chat/artifacts/artifactMeta.test.ts',
  'src/chat/artifacts/downloadAll.test.ts',
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
  'src/chat/generation/generationThread.test.tsx',
  'src/chat/artifacts/renderers/GeneratedImagePreview.test.tsx',
  'src/chat/artifacts/renderers/VideoPreview.test.tsx',
  'src/chat/artifacts/PreviewModal.test.tsx',
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
