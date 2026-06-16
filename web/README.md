# aura-web

The Aura operator cockpit frontend. A Vite 8 + React 19 + TypeScript single package
whose production build is committed to `internal/webui/dist/` and embedded into the
single Go binary via `//go:embed all:dist`. `aura serve` serves the embedded shell.

## Scripts

| Script                 | What it does                                                                 |
| ---------------------- | ---------------------------------------------------------------------------- |
| `npm run dev`          | Vite dev server                                                              |
| `npm run build`        | `generate-theme.mjs` → `tsc -b` → `vite build` into `../internal/webui/dist` |
| `npm run lint`         | ESLint flat config, `--max-warnings=0` (zero-warning gate)                   |
| `npm run format:check` | Prettier check                                                               |
| `npm run typecheck`    | `tsc --noEmit`                                                               |
| `npm run test`         | Vitest + RTL (jsdom)                                                         |
| `npm run test:e2e`     | Playwright (boots `aura serve`)                                              |

## Build output

`npm run build` writes to `../internal/webui/dist/` (NOT `web/dist/`). Go `//go:embed`
is package-relative and cannot reach `../web/dist`, so the committed embed source is
co-located with `internal/webui/embed.go`. Sourcemaps are disabled so the committed
bytes stay byte-stable for the `web-dist-freshness` CI gate (23-03).

## Design tokens

`tokens/tokens.json` is the single hand-authored source for the dark-operator palette
and the `compact|operator|review` density tiers (default `operator`). `tokens/generate-theme.mjs`
(a tiny ~50-LOC generator, NOT Style Dictionary) emits `src/styles/theme.css` (the
Tailwind 4 `@theme` block + per-`data-density` override blocks). The theme-before-paint
script is INLINE+SYNCHRONOUS in `index.html` (it must run before first paint, so it is
hand-maintained there rather than bundled), keyed on `aura.theme` / `aura.density`
localStorage (shared with `src/theme/applyTheme.ts`). The generator asserts that the
inline script's keys/defaults still match `tokens.json $meta` and fails the build on
drift, so the single source stays authoritative. Regenerate after editing tokens:

```bash
node tokens/generate-theme.mjs
```

`src/styles/theme.css` is GENERATED — do not hand-edit.

## Brand & PWA icons

The brand/PWA icon set in `public/` is pre-generated once from the repo-root
`public/Logo.png` (1024×1024) and committed — there is no build-time `sharp` dependency.
To regenerate (e.g. after a logo change), run a one-off `sharp` script from a scratch
directory (sharp installed transiently, NOT added to this package):

```bash
# from a throwaway dir with `npm install sharp`:
node - "$REPO/public/Logo.png" "$REPO/web/public" <<'JS'
import sharp from 'sharp';
const [SRC, OUT] = process.argv.slice(2);
const BG = { r: 0x0b, g: 0x0e, b: 0x14, alpha: 1 };
const contain = { fit: 'contain', background: { r: 0, g: 0, b: 0, alpha: 0 } };
const square = (size, file, flat) =>
  (flat
    ? sharp(SRC).resize(size, size, contain).flatten({ background: BG })
    : sharp(SRC).resize(size, size, contain)
  ).png().toFile(`${OUT}/${file}`);
await square(256, 'logo.png', false);            // header source (transparent)
await square(180, 'apple-touch-icon.png', true);
await square(192, 'pwa-192.png', true);
await square(512, 'pwa-512.png', true);
// maskable: logo inside an 80% safe zone on the brand background
const inner = Math.round(512 * 0.8);
const pad = Math.round((512 - inner) / 2);
const logo = await sharp(SRC).resize(inner, inner, contain).png().toBuffer();
await sharp({ create: { width: 512, height: 512, channels: 4, background: BG } })
  .composite([{ input: logo, top: pad, left: pad }])
  .png().toFile(`${OUT}/pwa-maskable-512.png`);
JS
```

`favicon.svg` embeds a 64×64 PNG of the logo (base64) on the brand background.
