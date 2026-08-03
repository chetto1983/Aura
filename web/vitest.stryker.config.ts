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
  'src/graph/__tests__/SigmaCanvas_labels.test.ts',
  'src/graph/__tests__/SigmaCanvas_reducers.test.ts',
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
  'src/chat/__tests__/toolGrouping.test.ts',
  'src/chat/__tests__/durationFormat.test.ts',
  'src/chat/__tests__/ReasoningPill.test.tsx',
  'src/audit/__tests__/auditPairing.test.ts',
  'src/conversations/__tests__/exportConversation.test.ts',
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
