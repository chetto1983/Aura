import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import importX from 'eslint-plugin-import-x';
import prettier from 'eslint-config-prettier';
import globals from 'globals';

export default tseslint.config(
  {
    ignores: [
      'dist/**',
      'coverage/**',
      'playwright-report/**',
      'test-results/**',
      'src/styles/theme.css',
      // Wave-0 RED stubs: these import the not-yet-built AppShell / served shell,
      // so type-aware lint degrades to `any` and fires false positives. They are
      // RED at the vitest/playwright layer by design. Remove these two ignores
      // once 23-02 creates src/AppShell.tsx and 23-03 wires the served shell.
      'src/__tests__/AppShell.test.tsx',
      'e2e/shell.spec.ts',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  importX.flatConfigs.recommended,
  {
    // import-x resolution rules are redundant with tsc (which owns module
    // resolution) and its TS-resolver interface conflicts with import-x@4;
    // no-named-as-default* are false positives on the canonical flat-config
    // default-import-of-an-eslint-plugin pattern. Keep the import-order value.
    rules: {
      'import-x/no-unresolved': 'off',
      'import-x/namespace': 'off',
      'import-x/no-named-as-default': 'off',
      'import-x/no-named-as-default-member': 'off',
      'import-x/order': [
        'warn',
        {
          groups: ['builtin', 'external', 'internal', 'parent', 'sibling', 'index'],
          'newlines-between': 'never',
        },
      ],
    },
  },
  {
    files: ['**/*.{ts,tsx,mjs}'],
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh, 'jsx-a11y': jsxA11y },
    rules: {
      ...reactHooks.configs['recommended-latest'].rules,
      ...jsxA11y.flatConfigs.recommended.rules,
      'react-refresh/only-export-components': 'warn',
    },
  },
  {
    files: [
      '**/*.config.{ts,js,mjs}',
      'tokens/*.mjs',
      'vite.config.ts',
      'vitest.config.ts',
      'playwright.config.ts',
    ],
    ...tseslint.configs.disableTypeChecked,
  },
  {
    files: ['**/*.config.{ts,js,mjs}', 'tokens/*.mjs'],
    languageOptions: {
      globals: { ...globals.node },
      parserOptions: { projectService: false },
    },
  },
  prettier,
);
