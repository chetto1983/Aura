import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig, type Plugin, type ResolvedConfig } from 'vite';
import react, { reactCompilerPreset } from '@vitejs/plugin-react';
import babel from '@rolldown/plugin-babel';
import tailwindcss from '@tailwindcss/vite';

const pwaManifest = {
  name: 'Aura',
  short_name: 'Aura',
  start_url: '/',
  display: 'standalone',
  background_color: '#131314',
  theme_color: '#131314',
  lang: 'en',
  scope: '/',
  icons: [
    { src: 'pwa-192.png', sizes: '192x192', type: 'image/png' },
    { src: 'pwa-512.png', sizes: '512x512', type: 'image/png' },
    { src: 'pwa-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
  ],
} as const;

interface PrecacheEntry {
  url: string;
  revision: string;
}

function revisionOf(source: string | Uint8Array): string {
  return createHash('sha256').update(source).digest('hex').slice(0, 16);
}

function publicAssetEntries(publicDir: string): PrecacheEntry[] {
  if (!existsSync(publicDir)) {
    return [];
  }

  return readdirSync(publicDir)
    .filter((fileName) => statSync(join(publicDir, fileName)).isFile())
    .map((fileName) => {
      const source = readFileSync(join(publicDir, fileName));
      return { url: `/${fileName}`, revision: revisionOf(source) };
    });
}

function auraPwaPlugin(): Plugin {
  let config: ResolvedConfig;

  return {
    name: 'aura-pwa',
    apply: 'build',
    configResolved(resolvedConfig) {
      config = resolvedConfig;
    },
    transformIndexHtml() {
      return [
        {
          tag: 'link',
          attrs: { rel: 'manifest', href: '/manifest.webmanifest' },
          injectTo: 'head',
        },
        {
          tag: 'script',
          attrs: { id: 'aura-pwa:register-sw', src: '/registerSW.js' },
          injectTo: 'head',
        },
      ];
    },
    generateBundle(_, bundle) {
      const manifestSource = JSON.stringify(pwaManifest);
      const registerSource =
        "if('serviceWorker' in navigator){window.addEventListener('load',()=>{navigator.serviceWorker.register('/sw.js',{scope:'/'})})}\n";

      this.emitFile({ type: 'asset', fileName: 'manifest.webmanifest', source: manifestSource });
      this.emitFile({ type: 'asset', fileName: 'registerSW.js', source: registerSource });

      const bundleEntries = Object.values(bundle)
        .filter((item) => !item.fileName.endsWith('.map'))
        .map((item) => {
          const source = item.type === 'chunk' ? item.code : item.source;
          return { url: `/${item.fileName}`, revision: revisionOf(String(source)) };
        });

      const generatedEntries = [
        { url: '/index.html', revision: 'app-shell' },
        { url: '/manifest.webmanifest', revision: revisionOf(manifestSource) },
        { url: '/registerSW.js', revision: revisionOf(registerSource) },
      ];

      const entries = [
        ...bundleEntries,
        ...generatedEntries,
        ...publicAssetEntries(config.publicDir),
      ].sort((left, right) => left.url.localeCompare(right.url));
      const cacheName = `aura-precache-${revisionOf(JSON.stringify(entries))}`;
      const swSource = `const CACHE_NAME=${JSON.stringify(cacheName)};
const PRECACHE=${JSON.stringify(entries)};
self.addEventListener('install',(event)=>{
  event.waitUntil(caches.open(CACHE_NAME).then((cache)=>Promise.all(PRECACHE.map((entry)=>cache.add(entry.url).catch(()=>undefined)))).then(()=>self.skipWaiting()));
});
self.addEventListener('activate',(event)=>{
  event.waitUntil(caches.keys().then((keys)=>Promise.all(keys.filter((key)=>key.startsWith('aura-precache-')&&key!==CACHE_NAME).map((key)=>caches.delete(key)))).then(()=>self.clients.claim()));
});
self.addEventListener('fetch',(event)=>{
  const request=event.request;
  if(request.method!=='GET') return;
  const url=new URL(request.url);
  if(url.origin!==self.location.origin) return;
  if(request.mode==='navigate'){
    event.respondWith(fetch(request).catch(()=>caches.match('/index.html')));
    return;
  }
  event.respondWith(caches.match(request).then((cached)=>cached||fetch(request)));
});
`;

      this.emitFile({ type: 'asset', fileName: 'sw.js', source: swSource });
    },
  };
}

function auraCssCompatLintPlugin(): Plugin {
  const legacyTextSizeAdjust = /-webkit-text-size-adjust:\s*100%;?/g;

  return {
    name: 'aura-css-compat-lint',
    apply: 'build',
    enforce: 'post',
    generateBundle(_, bundle) {
      for (const item of Object.values(bundle)) {
        if (
          item.type !== 'asset' ||
          !item.fileName.endsWith('.css') ||
          typeof item.source !== 'string'
        ) {
          continue;
        }

        // Tailwind preflight emits this legacy normalization, which webhint flags
        // as a hard compat error in Edge Tools. The app does not rely on it.
        const cleaned = item.source.replace(legacyTextSizeAdjust, '');
        if (cleaned === item.source) {
          continue;
        }

        const oldFileName = item.fileName;
        const hash = revisionOf(cleaned).slice(0, 8);
        const newFileName = oldFileName.match(/-[A-Za-z0-9_-]+\.css$/u)
          ? oldFileName.replace(/-[A-Za-z0-9_-]+\.css$/u, `-${hash}.css`)
          : oldFileName.replace(/\.css$/u, `-${hash}.css`);

        item.source = cleaned;
        item.fileName = newFileName;

        for (const entry of Object.values(bundle)) {
          if (entry.type === 'chunk') {
            entry.code = entry.code.split(oldFileName).join(newFileName);
            continue;
          }
          if (typeof entry.source === 'string') {
            entry.source = entry.source.split(oldFileName).join(newFileName);
          }
        }
      }
    },
  };
}

// Plugin order is load-bearing: the React Compiler babel pass MUST precede
// react(), else only Oxc's JSX transform runs and the Compiler silently no-ops
// (RESEARCH Correction #2 / Pitfall 1). @rolldown/plugin-babel exports the plugin
// as default and takes { presets } (RESEARCH's named `babel`/`babelConfig` shape
// predates the shipped 0.2.3 API).
export default defineConfig({
  plugins: [
    babel({ include: /\.[jt]sx?$/, presets: [reactCompilerPreset()] }),
    react(),
    tailwindcss(),
    auraCssCompatLintPlugin(),
    auraPwaPlugin(),
  ],
  build: {
    // Go //go:embed is package-relative, so the committed dist co-locates with
    // internal/webui/embed.go (23-01 Deviation #1). This overwrites the placeholder.
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    sourcemap: false,
    rolldownOptions: {
      checks: {
        // The React Compiler intentionally runs through @rolldown/plugin-babel.
        // That makes plugin timing telemetry noisy in Docker/CI logs while not
        // indicating a build failure. Keep real correctness warnings enabled.
        pluginTimings: false,
      },
      output: {
        // Keep heavy chat/display dependencies off route chunks. Shiki stays lazy,
        // but splits by concern so one highlighter path cannot breach Vite's 500 kB
        // chunk-warning threshold.
        codeSplitting: {
          groups: [
            {
              name: 'assistant-ui',
              test: /[\\/]node_modules[\\/](@assistant-ui|assistant-stream)[\\/]/,
            },
            {
              name: 'markdown',
              test: /[\\/]node_modules[\\/](@types[\\/]hast|hast|hast-util-|mdast-util-|micromark|react-markdown|rehype-|remark-|unified|unist-util-|vfile)[\\/]/,
            },
            {
              name: 'shiki-core',
              test: /[\\/]node_modules[\\/](shiki|@shikijs[\\/](core|engine-javascript|primitive|types|vscode-textmate))[\\/]/,
            },
            {
              name: 'shiki-themes',
              test: /[\\/]node_modules[\\/]@shikijs[\\/]themes[\\/]/,
            },
            {
              name: 'shiki-lang-js',
              test: /[\\/]node_modules[\\/]@shikijs[\\/]langs[\\/]dist[\\/](java|type)script\.mjs$/,
            },
            {
              name: 'shiki-lang-web',
              test: /[\\/]node_modules[\\/]@shikijs[\\/]langs[\\/]dist[\\/](css|html|json|markdown|yaml)\.mjs$/,
            },
            {
              name: 'shiki-lang-tools',
              test: /[\\/]node_modules[\\/]@shikijs[\\/]langs[\\/]dist[\\/](bash|go|python|shellscript|sql)\.mjs$/,
            },
          ],
        },
      },
    },
  },
});
