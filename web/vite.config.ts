import { defineConfig } from 'vite';
import react, { reactCompilerPreset } from '@vitejs/plugin-react';
import { babel } from '@rolldown/plugin-babel';
import tailwindcss from '@tailwindcss/vite';
import { VitePWA } from 'vite-plugin-pwa';

// Plugin order is load-bearing: babel(reactCompilerPreset) MUST precede react(),
// else only Oxc's JSX transform runs and the React Compiler silently no-ops
// (RESEARCH Correction #2 / Pitfall 1).
export default defineConfig({
  plugins: [
    babel({ include: /\.[jt]sx?$/, babelConfig: reactCompilerPreset() }),
    react(),
    tailwindcss(),
    VitePWA({
      registerType: 'autoUpdate',
      strategies: 'generateSW',
      includeAssets: ['favicon.svg', 'apple-touch-icon.png'],
      manifest: {
        name: 'Aura',
        short_name: 'Aura',
        theme_color: '#0B0E14',
        background_color: '#0B0E14',
        display: 'standalone',
        icons: [
          { src: 'pwa-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'pwa-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
    }),
  ],
  build: {
    // Go //go:embed is package-relative, so the committed dist co-locates with
    // internal/webui/embed.go (23-01 Deviation #1). This overwrites the placeholder.
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    sourcemap: false,
  },
});
