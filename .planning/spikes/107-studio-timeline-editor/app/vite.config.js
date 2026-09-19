import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// One chunk per npm package, so the build report attributes gzip bytes to each library.
const packageOf = (id) => id.replaceAll('\\', '/').match(/node_modules\/((?:@[^/]+\/)?[^/]+)/)?.[1];
const SHARED = new Set(['react', 'react-dom', 'scheduler']);

// Writes every module's rendered code per chunk to modules.json, so run-size.mjs can gzip each package's
// share of a chunk (rolldown merges some groups, e.g. mediabunny into renderer-browser).
const moduleReport = {
  name: 'module-report',
  generateBundle(_options, bundle) {
    const chunks = Object.values(bundle).filter((c) => c.type === 'chunk').map((c) => ({
      file: c.fileName,
      modules: Object.entries(c.modules).map(([id, m]) => ({ pkg: packageOf(id) ?? 'app', id: id.replaceAll('\\', '/').split('node_modules/').pop(), code: m.code ?? '' })),
    }));
    this.emitFile({ type: 'asset', fileName: 'modules.json', source: JSON.stringify(chunks) });
  },
};

// STUB_GOOGLE_FONTS=1: VideoFlow statically imports its Google Fonts registry (googlefonts.json) for
// buildFontUrl, which the local loadFont override never calls. Replace it with an empty registry.
const stubGoogleFonts = {
  name: 'stub-google-fonts',
  load(id) {
    if (id.replaceAll('\\', '/').endsWith('@videoflow/renderer-browser/dist/googlefonts.json')) return '{"items":[]}';
  },
};

export default defineConfig({
  plugins: [react(), moduleReport, ...(process.env.STUB_GOOGLE_FONTS ? [stubGoogleFonts] : [])],
  publicDir: '../media',
  build: {
    outDir: '../out/dist',
    emptyOutDir: true,
    copyPublicDir: false,
    chunkSizeWarningLimit: 5000,
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [{ test: /node_modules/, name: (id) => { const p = packageOf(id); return p && !SHARED.has(p) ? `pkg-${p.replace('/', '__')}` : null; } }],
        },
      },
    },
  },
});
