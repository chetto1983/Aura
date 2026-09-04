import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  // The `@/` alias mirrors vite.config.ts + tsconfig.json so `@/components/ui/*`
  // and `@/lib/utils` resolve identically under the test runner.
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  // The chat SSE-reducer test imports the REAL captured AG-UI golden frames from
  // internal/agui/testdata/ (the same fixture the Go SSE tests use). That file is
  // a sibling of web/, so the dev-server fs guard must allow reading one level up.
  server: { fs: { allow: ['..'] } },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    setupFiles: ['./src/test/setup.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      // main.tsx is the bootstrap entry (createRoot + render) — behaviourally proven by
      // the Playwright E2E against the served shell, not unit-testable in jsdom.
      // The vendored registry components are upstream source: `npx shadcn add
      // @assistant-ui/model-selector` rewrites them verbatim on every update, so testing
      // them measures the registry, not us, and their untested bulk would drag a floor
      // that exists to keep OUR code covered. Excluded here for the same reason eslint,
      // knip, prettier and the LOC cap ignore them; ModelPicker, the code that USES them,
      // stays in the denominator.
      exclude: [
        'src/**/*.{test,spec}.{ts,tsx}',
        'src/test/**',
        'src/main.tsx',
        'src/components/model-selector.tsx',
        'src/components/ui/command.tsx',
        'src/components/ui/popover.tsx',
      ],
      // Frontend quality gate: parity with the Go backend's ≥85% floor (the suite fails
      // below it, so coverage can't silently regress).
      thresholds: {
        statements: 85,
        branches: 85,
        functions: 85,
        lines: 85,
      },
    },
  },
});
