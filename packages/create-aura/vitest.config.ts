import { configDefaults, defineConfig } from 'vitest/config';

const ledger = process.env.AURA_TEST_LEDGER_DIR;

export default defineConfig({
  test: {
    // With AURA_TEST_LEDGER_DIR set, a JSON report joins Vitest's own reporters, github-actions
    // included (docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.2).
    ...(ledger
      ? {
          reporters: [...configDefaults.reporters, 'json'],
          outputFile: { json: `${ledger}/vitest-create-aura.json` },
        }
      : {}),
    environment: 'node',
    coverage: {
      provider: 'v8',
      include: ['src/**/*.ts'],
      // bin.ts is the process entry point (argv -> runCli -> process.exitCode), the same
      // shape web/ excludes main.tsx for: nothing to unit-test past a call already covered
      // by exercising runCli itself.
      exclude: ['src/**/__tests__/**', 'src/bin.ts'],
      // Parity with the Go backend's and web/'s >=85% floor (see CLAUDE.md Coverage floor).
      thresholds: {
        statements: 85,
        branches: 85,
        functions: 85,
        lines: 85,
      },
    },
  },
});
