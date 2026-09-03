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
      // Local-only byproducts: a stale install dir + Stryker's sandbox/report output.
      'node_modules.broken/**',
      '.stryker-tmp/**',
      'reports/**',
      // Plain Node test-infra script (a local TLS terminator for the WebKit e2e
      // project); not part of any TS project, so exclude it from typed linting.
      'e2e/https-proxy.mjs',
      // Vendored registry components, installed verbatim with `npx shadcn add`
      // (@assistant-ui/model-selector and the cmdk/popover primitives it pulls in).
      // They are upstream source, refreshed by re-running the installer: linting them
      // to Aura's house style would mean re-fixing them on every update, and a local
      // edit here is a change you lose without noticing.
      'src/components/model-selector.tsx',
      'src/components/model-selector.aui.tsx',
      'src/components/ui/command.tsx',
      'src/components/ui/popover.tsx',
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
      'import-x/named': 'off',
      'import-x/namespace': 'off',
      // Also a resolution rule: its legacy node resolver crashes on exports-map-only
      // packages (read-excel-file/universal), and tsc already validates default exports.
      'import-x/default': 'off',
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
      'scripts/*.mjs',
      'vite.config.ts',
      'vitest.config.ts',
      'playwright.config.ts',
    ],
    ...tseslint.configs.disableTypeChecked,
  },
  {
    files: ['**/*.config.{ts,js,mjs}', 'tokens/*.mjs', 'scripts/*.mjs'],
    languageOptions: {
      globals: { ...globals.node },
      parserOptions: { projectService: false },
    },
  },
  {
    // shadcn/ui primitives co-locate a CVA variant helper (e.g. `buttonVariants`)
    // with the component so other surfaces can compose it (`cn(buttonVariants(...))`).
    // That is the canonical new-york shape; allow the named variant export without
    // tripping react-refresh's component-only rule for this directory only.
    files: ['src/components/ui/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': [
        'warn',
        { allowExportNames: ['buttonVariants', 'badgeVariants'] },
      ],
    },
  },
  {
    // Tests keep defensive runtime guards (e.g. `node.textContent ?? ''`) even
    // where the resolved DOM types say the value is non-null — the guard hardens
    // the test across jsdom/browser type drift without weakening any assertion.
    files: ['src/**/*.{test,spec}.{ts,tsx}', 'e2e/**/*.{ts,tsx}'],
    rules: { '@typescript-eslint/no-unnecessary-condition': 'off' },
  },
  prettier,
);
