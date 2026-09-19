import { log } from './log.js';

// VideoFlow resolves every family through `renderer.loadFont(name)`: the renderer's own init calls it
// for its hard-coded default 'Noto Sans', and every text layer calls it for its `fontFamily`. The
// stock implementation fetches fonts.googleapis.com. Replacing that one public method on the
// instance keeps the whole pipeline (document.fonts + FontEmbedder's SVG inlining, which reads
// `loadedFonts`) but points it at same-origin stylesheets. There is no config option for this.
export const LOCAL_FONTS = {
  'Noto Sans': '/fonts/noto-sans-alias.css',
  'Atkinson Hyperlegible Next': '/fonts/atkinson.css',
};

export function useLocalFonts(renderer) {
  renderer.loadFont = async (name) => {
    // DomRenderer declares `loadedFonts` TS-private; it is a plain property at runtime and the
    // FontEmbedder holds a live reference to the same object.
    if (name in renderer.loadedFonts) return;
    const href = LOCAL_FONTS[name];
    if (!href) {
      log('font', `no local font for "${name}" — left to the fallback stack`);
      return;
    }
    renderer.loadedFonts[name] = href;
    if (!document.querySelector(`link[data-local-font="${name}"]`)) {
      const link = Object.assign(document.createElement('link'), { rel: 'stylesheet', href });
      link.dataset.localFont = name;
      const loaded = new Promise((resolve, reject) => { link.onload = resolve; link.onerror = reject; });
      document.head.appendChild(link);
      await loaded;
    }
    await document.fonts.load(`1em "${name}"`);
    log('font', `local font "${name}" ← ${href}`);
  };
  return renderer;
}
