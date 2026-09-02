import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
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
