# Adobe Express video editor: UX tour

Toured on 2026-09-19 with Playwright + Chrome, it-IT locale, desktop 1440×900 and phone 390×844 (`isMobile`).
No login, no account, no payment. The only uploads were `clip.mp4` (320×180, 4 s, 163,832 B) and `photo.png`.
Every screenshot file named below is in this folder. `*.yml` files hold the accessibility tree captured
beside the screenshot of the same name.

**Access notes**
- Headless Chrome gets `ERR_HTTP2_PROTOCOL_ERROR` from adobe.com, and `curl` hangs. The pages load once the
  user agent is a normal Chrome one. helpx.adobe.com returns 403 to WebFetch, so I read it through the browser
  (`09-helpx.txt`).
- The full editor has **no hard login wall**. The sign-in modal offers a small "O Continua come ospite"
  (continue as guest) link. A guest can do every edit. The wall comes at **Download**, **Share** and **Save**.
  I stopped at that wall.

---

## (a) Catalogue by area

### A1. Feature page: https://www.adobe.com/it/express/feature/video/editor

Screens: `01-feature-desktop-top.png`, `01-feature-desktop-full.png`, `01-feature-phone-top.png`,
`01-feature-phone-full.png`. Text dump: `01-feature-desktop.json`.

| Advertised capability | How it is presented |
|---|---|
| Upload and edit | Hero drop zone: "Trascina un video o sfoglia per caricarlo", **Carica il tuo video** button, "Dimensione massima del file: 1 GB", pills "Uso gratuito · Non è richiesta una carta di credito", fine print "Caricando… accetti i Termini di Utilizzo". The upload goes **straight into the full editor**. |
| 3-step how-to | Cards on a purple gradient: 1. upload media, 2. pick a template, 3. "Ritaglia, taglia o dividi" (crop, trim, split), add an Adobe Stock soundtrack, download as MP4. |
| Trim / crop / refine, **remove background noise**, royalty-free music | "Modifica i video con facilità" section. The illustration shows a scene filmstrip, two audio tracks ("Epilogue", "Upbeat Funk Acoustic") and an Audio panel. |
| Resize for social in one click, MP4, **content scheduler** | "Condividilo a modo tuo". The illustration shows a *Resize* checklist: Instagram square post / story / reel, TikTok video, Facebook post, Instagram carousel, YouTube video. |
| Animated effects on any element | "Distinguiti con gli effetti animati". The illustration shows an "In Animation" panel with a grid of ABC tiles. |
| Templates, stock video, audio and SFX | "Tantissimi modelli gratuiti", shown as a template collage. |
| Formats | Chip row "Fai di più": Facebook, Instagram, TikTok, YouTube clip, Marketing, Slideshow, Animation, Outro, Intro, Promo. |
| Merge, speed, TikTok sizing | Only as FAQ links (resize / merge / change-speed). |
| Quick-action family | Listed in the "Modifica" mega-menu: Video→GIF, Crop, Trim, Resize, Merge, Convert to MP4, Animate characters, Caption video, Remove video background, **Clip maker (long→short, AI)**. |

- **Media:** no `<video>` and no iframe on the page. The five illustrations are static webp mock-ups of the editor
  UI, described above; the full-page screenshots cover them, so no frame grabs were needed.
- **Localization bug:** the Italian page renders the mega-menu in **Portuguese** ("Cortar vídeo", "Combinar vídeo"…).
- **Phone:** there is **no upload widget**. A bottom sheet says "Ottieni l'esperienza completa nell'app" with
  **Ottieni gratis** (install the app) versus "Versione Web: Continua". The web "Continua" is an
  `adobesparkpost.app.link` deep link and did nothing in the emulated browser
  (`07-editor-phone-00-feature-app-sheet.png`).

### A2. Quick actions without login

Every quick action is an iframe served from `quick-actions.express.adobe.com/<action>?authMode=delayed&embed=true`,
set inside a long SEO page. They share one layout:
- **Upload:** a drop zone.
- **Preview:** a player on the left, with a play button in the middle.
- **Controls:** a column on the right.
- **Actions:** **Scarica** (secondary, outline) next to **Apri in Adobe Express** (primary, filled), with the
  "Utilizzo gratuito · Non è richiesta la carta di credito" pills.
- **After download:** a menu, "Esplora altre azioni rapide, è gratis", lists GIF / Crop / Trim / Resize / MP4 /
  Animate characters / Caption, plus a floating **Valutaci** (rate us) star.

| Action (URL under /it/express/feature/video/) | Controls after uploading clip.mp4 | Screens |
|---|---|---|
| `trim` | **Inizio / Fine** timecode fields (00:00:00 / 00:00:04). A filmstrip with start and end trim handles, visible on phone. **Layout** dropdown (Orizzontale 16:9…). **Silenzia** toggle. **Download works without login**: `export-trim-Adobe Express - clip.mp4`, 511,824 B from a 163,832 B source with no trim applied (a 3.1× re-encode). While it downloads, the player shows a spinner overlay: "Adobe Express sta scaricando il tuo file…". | `04-qa-trim-desktop-0-landing/-3`, `05-qa-trim-desktop-after-download-1/-3`, `08-qa-trim-phone-controls.png`, `04-qa-trim-phone-*` |
| `crop` | **Proporzioni** tiles: 16:9 Orizzontale, 9:16 Verticale, 1:1 Quadrato, Forma Libera. A crop box with a rule-of-thirds grid on the preview. Silenzia. | `04-qa-crop-desktop-3`, `04-qa-crop-phone-3` |
| `resize` | A platform dropdown ("Instagram") over tiles that print the **exact pixel size**: Quadrato 1080×1080, Storia/Reel 1080×1920, Verticale 4:5 1080×1350, Orizzontale 1.97:1 1200×608, IGTV 4:5 and 9:16. Silenzia. | `04-qa-resize-desktop-3` |
| `convert/video-to-gif` | **Dimensione file** radios: Grande (alta qualità), **Media** (default), Piccola (bassa qualità). Layout dropdown. | `04-qa-convert_video-to-gif-desktop-3` |
| `convert/mp4` | Silenzia only, then Scarica / Apri. | `04-qa-convert_mp4-desktop-3` |
| `merge` | The input accepts **multiple** files, video + jpg/png. With one clip: Layout "Originale", and audio **muted by default** ("Riattiva audio" is pressed). With clip + photo: a **2-scene strip** ("Scena 1 di 2"), a per-scene menu (**Taglia, Silenzia, Riempi schermo, Duplica, Elimina**), **+ Aggiungi media**, player 0:00 / 0:10. It then **failed**: "Si è verificato un problema… UUID", and Scarica stayed disabled. | `04-qa-merge-desktop-1/-3`, `06-qa-merge-two-desktop-1/-2`, `06-qa-merge-two-desktop.txt` |
| `change-speed` | No widget. The CTA "Carica il tuo video" opens the full editor. The illustration shows a filmstrip with a "150%" badge. | `04-qa-change-speed-desktop-0-landing` |
| `add-caption` | No widget. The CTA goes to the editor. The illustration shows sticker-style captions. | `04-qa-add-caption-desktop-0-landing` |
| `animation/audio` (animate characters) | No widget on the page. The CTA goes to the editor. | `04-qa-animation_audio-desktop-0-landing` |
| `reverse`, `add-audio` | **404**: these are not offered as quick actions. | `04-qa-reverse-…`, `04-qa-add-audio-…` |

### A3. Full editor (guest): https://new.express.adobe.com/new?…&sceneline=true

**Entry.** Uploading on the feature page first sends the file to Adobe storage (`frictionlessUploadAssetId=urn:aaid:sc:DEU1:…`).
It then opens the editor with the **canvas sized to the source** (`width=320&height=180`) and the sign-in modal on top
(Google / Facebook / Apple / e-mail, then a small "O Continua come ospite" link). On first load three overlays
stack: the cookie banner, the sign-in modal and a "Suggerimento rapido" coach mark.
Screens: `02-editor-upload-desktop-*`, `03-editor-desktop-00-signin-wall.png`, `03-editor-desktop-01-after-guest.png`.

**Shell** (`03-editor-desktop-02-shell.png`, `…-02-aria.yml`)
- **Header (dark):** logo · File ▾ · title "Senza titolo…" · gradient **Inizia la prova gratuita** pill · zoom % ▾ ·
  undo/redo · Invita · Scopri (lightbulb with a red dot) · Scarica · **Condividi** (blue) · **Registrati per salvare**.
- **Left rail (9):** Cerca · Aggiungi contenuto (a flyout: Multimedia / Elementi / Grafici e griglie / **IA generativa**) ·
  Testo · Carica · Le tue risorse · Marchi · Modelli · Stili · Add-on.
- **Left contextual panel:** follows the selection. A video layer gives tabs **Modifica / Effetti / Animazione**.
- **Canvas:** a floating contextual toolbar with Modifica (panel toggle) · Sostituisci · volume (a **mute toggle**,
  the icon becomes speaker-slash) · Ritaglia · Duplica · lock · delete · "…" (copy/paste/duplicate/delete +
  arrange order with Ctrl+]/[). The canvas has a rotate handle.
- **Right column:** a **layer stack as thumbnails**, one per layer, reorderable with keyboard shortcuts.
- **Timeline:**
  - Header: CC (captions) · split-at-playhead · current / total time · play · **Mostra tempistica** (view switch) ·
    zoom −/+ · hide timeline.
  - Body: a ruler, then layer rows, then the **scene track** (filmstrip thumbnails, trim handles, "Menu scena",
    a "+" add-media tile), then an audio lane ("Aggiungi audio").

**Video panel** (`03-editor-desktop-02-shell`, `-03-video-effects`, `-04-video-animation`, `-05-video-adjust`)
- **Modifica:**
  - AI tiles: Rimuovi sfondo 👑, Riempi con video, Ottimizza parlato 👑, **Estrai audio** (appears once
    processing ends), Modifica divisione video.
  - Volume slider plus a numeric %.
  - **Velocità** slider with detent ticks, 100% at the centre.
  - **Regola** sub-panel, with slider + number box + reset for each: Luce (Contrasto, Luminosità, Luci, Ombre),
    Colore (Saturazione plus a temperature slider **mislabelled "Reimposta"**), Dettagli (Nitidezza, Sfocatura),
    and "Reimposta tutto".
  - Angoli (corner radius), Posizione, Opacità.
- **Effetti:** Toni: Nessuno, Scala di grigi, Scurisci, Duotone, "Visualizza tutto".
- **Animazione:**
  - **Anima tutto** ("anima il tuo intero progetto in un solo click").
  - Segmented **Entrata / Ciclo / Uscita** (enter / loop / exit).
  - Animated thumbnail tiles in groups:
    - Visibilità: Cerchio, Fade, Porta fienile, Sfarfallio.
    - Spostamento: Ascesa, Capriola, Discesa, Elastico, Ruota, Scivolata, Scorrimento.
    - Ridimensionamento: Apparizione, Crescita, Riduzione.
    - Aspetto: Flash, Otturatore, Scala di grigi, Sfoca.

**Crop mode** (`03-editor-desktop-06-crop.png/.yml`): a separate mode with **Annulla / Fatto**.
- Ratios: Forma Libera, Originale, Quadrato, Storia, Widescreen, Orizzontale, Verticale.
- **Forme** (mask shapes): circle, oval, half-circle, rounded square, parallelogram, rhombus, 2 triangles, heart,
  hexagon, pentagon, star.

**Timeline model** (`-07-scene-menu`, `-08-layer-menu`, `-09-timing-view`, `-11-split`)
- **"Mostra la tempistica per":** Tutti i livelli / Livello selezionato / Solo scene.
- **Scene menu:** Copia · Incolla · Duplica · Rimuovi durata · **Applica la durata a tutto** · Dividi (S).
- **Layer menu:** Copia · Incolla · Duplica · Elimina · Estrai audio · **Montaggio con scivolamento** (slip edit) ·
  Blocca · Blocco: Permetti sostituzione · Sostituisci (^R) · Dividi (S).
- **Split at playhead** on a selected video layer made **two stacked layer rows inside the same scene** (0–2 s and
  2–4 s), not two scenes. A toast "Registrati o accedi per salvare il file" then sat over the canvas toolbar
  (`-11-split.png`).
- **Help pages:**
  - Scenes can be trimmed, reordered and multi-selected. Scene view is for bulk actions ("mute all videos and apply
    duration to all scenes"). "All layers (Beta)… available only on desktop" gives "trim, split, and move layers
    within a scene". https://helpx.adobe.com/express/web/create-and-edit-videos/create-videos/add-scenes.html
  - Per-layer trim handles and dragging a layer in time:
    https://helpx.adobe.com/express/web/video-creation-and-editing/edit-videos/adjust-layer-timing.html
  - **Timing indicator dots** above the timeline reveal timed objects:
    https://helpx.adobe.com/express/web/create-and-edit-videos/organize-clips/locate-timed-objects.html
  - Layers panel, lock, arrange: https://helpx.adobe.com/express/web/arrange-layers-and-pages/layers.html
  - July 2026 release notes (resize captions on canvas and apply to all, caption fonts, Photoshop-style filters on
    video, "Bounce In" animation, ProRes 4444 transparency for Enterprise):
    https://helpx.adobe.com/express/web/whats-new/release-notes.html

**Text** (`-14-text-panel`, `-15/16-text-*`, `-17-text-animation`, `-18-text-effects`)
- **Panel:** **Aggiungi testo** · search · **Stili di testo** (Titolo / Intestazione / Corpo / Dettagli) · premade
  lockups (Pulsanti, Segnali ed etichette, Fumetti; some 👑).
- **A new text layer spans the whole scene** as its own timeline row.
- **Modifica:**
  - **"Font consigliati" carousel rendering the operator's own text**.
  - Style preset ▾ · family ▾ · weight ▾ · size stepper.
  - B / I / U / link / align / list / ….
  - Colour, swap, background, stroke.
  - Layout: **Standard / Layout testo (circle) / Flusso testo (path)**.
  - **Riscrivi** (AI rewrite) · Posizione · Opacità.
- **Effetti:** Genera effetti testo (AI variants) · Ombra (Classica, Uniforme, Sollevata, Bagliore, Alone, Netta,
  Sfocata, Personalizzata) · Forma (highlight, filled square / circle / capsule, "between two lines").
- **Animazione:** as for video, plus a **Dinamico** group (Deformazione, Rimbalzo) and **Digitazione** (typewriter).
- **Floating text toolbar:** B · I · U · duplicate · copy-style (paint roller) · lock · ….

**Other panels**
- **Stili** (`-21-styles`): tabs Consigliati / Colori / Font, with one-click font pairings
  ("Montserrat Black + Montserrat Bold + 1…").
- **Modelli** (`-20-templates`): **Genera modello** (AI), plus categories (Social media e annunci, Stampa…).
- **IA generativa** (`-28-gen-ai`): tabs Immagine / **Video** / Modello / …, "Genera Immagine", gallery,
  AI terms link.
- **Marchi** (`-27-brands`): "Crea facilmente contenuti in linea con il tuo marchio", Configurazione manuale,
  **Estrai marchio da un caricamento** (extract brand from an upload).
- **Audio** (`-26-add-audio`): a Multimedia panel with tabs Tutti / Foto / Video / **Musica** / **Effetti sonori**.
  - Carica da dispositivo · **Registra voiceover** · Filtra.
  - Music add-ons: TikTok Symphony Assistant, Soundstripe, NCS, AI Music…
  - Adobe Stock tracks by mood (Acustica, Sfondo), with inline play and long tag lists.
- **Didascalie / captions** (`-29-captions`): "Genera didascalie" with **Lingua parlata nel media** and
  **Fonte multimediale**, then Genera. The copy says captions "vengono generate in background" while you keep
  editing.
- **Add-on** marketplace (`-23-addons`): ZIPline, Lottie2Video, Gifinity, Vivideo ("Import AI-generated…"), GIF
  Generator, Gen Sound, cloud drives… and also LawWiz legal tools and GeoJSON charts.
- **Ridimensiona (page)** (`-30-page-selected`, `-31-resize`): 👑 premium.
  - Personalizzate: W×H + unit + lock.
  - Consigliate tiles: Instagram vertical 1080×1350, A4 flyer, Instagram story 1080×1920.
  - Sfoglia categories.
  - **Duplica e Ridimensiona** versus **Ridimensiona**. "Ridimensiona aggiornerà automaticamente il layout".
- **File menu** (`-32-file-menu`): Accedi · Crea nuovo · Duplica · Converti in presentazione ·
  Ridimensiona 👑 · Traduci · Scarica · Condividi · Stampa · Smarginatura · Righelli e guide · Segnala un abuso.

**Export** (`-34-download`, `-35-format-options`, `-36-resolution-options`, `-38-download-progress-*`)
- A header popover titled "Scarica".
- **Formato file:** PNG / JPG / PDF standard / Stampa PDF / **MP4** (✓ Consigliato) / GIF.
- **Risoluzione video:** Personalizzata (dimensione originale) / 720p / 1080p / **4K**. All four are offered for a
  320×180 source.
- **Scarica** then opens a modal: **"Accedi o registrati gratuitamente per scaricare il tuo file"**, with no guest
  path.
- **Condividi** gives the same wall ("…per condividere", illustrated with the content scheduler; `-33-share`).

### A4. Phone layout

- **Feature page:** stacked, no uploader, app-install sheet (see A1).
- **Quick action `trim`** (`04-qa-trim-phone-0-landing`, `-3`, `08-qa-trim-phone-controls.png`):
  - "Tocca per caricare un video".
  - Preview, then a **filmstrip with trim handles and a "4s" badge**; the fields and buttons sit below.
  - The cookie banner covers about half the viewport.
- **Editor** at `new.express.adobe.com/new?width=1080&height=1920&sceneline=true`, reached directly
  (`07-editor-phone-01-wall` … `-09-export`):
  - **Header:** icons only (logo · premium crown · undo · redo · share · download · ⋯).
  - **Canvas:** a 9:16 page. The landscape clip lands **letterboxed inside the portrait page**.
  - **Timeline:** collapsed. The row reads timing-view button · 0:00 ▶ 0:05 · collapse chevron. Below it, the
    **scene strip** (a "4s" badge on the thumbnail, a dashed "+" tile, a ♫+ add-audio pill under the scene).
  - **Bottom toolbar:** scrolls horizontally: Cerca, Carica, Ridimensiona 👑, Testo, Elementi, Multimedia, Modelli,
    Le tue risorse, Marchi, Colore di sfondo, Anima tutto, Livelli, Sostituzione rapida, Registra voiceover,
    Disegna forma, Grafici…
  - **The toolbar swaps with the selection:**
    - scene selected: **Durata · Duplica · Dividi · Stili · Colore di sfondo**;
    - clip layer selected: **Volume · Velocità · Ritaglia · Rimuovi sfondo 👑 · Effetti**.
  - Coach mark: "Tocca e tieni premuto qualsiasi cosa… per visualizzare le opzioni di modifica rapida"
    (long-press for a quick menu).
  - **Timing view on phone:** only Livello selezionato / Solo scene. There is **no All-layers view**, which matches
    helpx ("only on desktop").
  - **Export:** a **bottom sheet** with Formato file MP4 ✓ Consigliato, Risoluzione "1080p (dimensione originale)",
    and a full-width Scarica.
  - A persistent bottom bar reads **"Per salvare il tuo file · Accedi ✕"**.

---

## (b) Best things to take (ranked for an operator editing AI-generated clips and images)

**Baseline.** Aura (`web/src/mediaEdit/VideoEditor.tsx`) already has trim with Start/End fields and a filmstrip,
crop, rotate and mute, exported by Mediabunny's `Conversion`.

**What the engine already offers.** I checked the installed **mediabunny 1.58.1** (`dist/mediabunny.d.ts`).
`ConversionVideoOptions` already has:
- `width` / `height` / `fit: 'fill'|'contain'|'cover'`
- `flip`
- `frameRate`
- `quality`
- a per-frame `process(sample)` hook. Its doc lists "overlays, color transformations, or timestamp modifications".

There is no GIF output format, and `Conversion` takes **one input**. Where a tag says "engine", it names what Aura
lacks today. Where the dependency already has it, the tag says so.

1. **Output-size tiles that print pixels, with Fit vs Fill.**
   Adobe's resize quick action shows "Storia/Reel 1080×1920", "Verticale 1080×1350" and so on. The editor adds
   "Riempi con video" / "Riempi schermo". AI clips arrive at model-native sizes (832×480, 1280×720…) and almost
   always leave as 9:16, 1:1 or 4:5. One tap should give the right size, with a visible choice between letterbox
   (contain) and fill (cover).
   *[needs new engine capability: scaling / letterbox. It is already in mediabunny as `width/height/fit`, so this
   is wiring only]*
2. **Export sheet with 2 decisions: format and resolution, each with a recommended default.**
   Adobe shows MP4 ✓ Consigliato and "Original / 720p / 1080p". On phone it is a bottom sheet with one full-width
   button. Unlike Adobe, hide resolutions above the source's own size.
   *[UX pattern only for the MP4 path. Resolution presets reuse #1]*
3. **GIF output with a 3-level size/quality radio (Grande / Media / Piccola, default Media).**
   AI loops are shared as GIFs constantly, and three plain-language levels beat fps/colour/dither knobs.
   *[needs new engine capability: GIF encoding. mediabunny has no GIF output format]*
4. **Speed as a slider with detents around 100%** (Adobe's Velocità, and the phone's clip toolbar).
   AI clips are often slow-motion or a little too fast. 0.5× / 1× / 1.5× / 2× detents cover most needs.
   *[needs new engine capability: speed / retime. Video can be retimed through `process` timestamps; audio needs
   tempo change or must be dropped]*
5. **Split at playhead (key `S`) plus delete a segment**, to cut a glitch out of the middle of a generation.
   Unlike Adobe, the split should yield **two scenes**, not two stacked layers.
   *[needs new engine capability: multi-segment. Concatenating kept ranges goes beyond a single `Conversion` trim]*
6. **Scene strip for stitching generations:**
   - thumbnails with a **duration badge** ("4s"), drag to reorder, a dashed "+" tile to add media;
   - a per-scene menu: Trim, Mute, Fill screen, Duplicate, Delete;
   - stills get a default duration, as in Adobe's merge quick action.
   This is the natural unit for "shot 1 + shot 2 + shot 3".
   *[needs new engine capability: multi-clip concat and still-to-video]*
7. **Selection-driven contextual toolbar on phone.** Scene selected: Duration / Duplicate / Split. Clip selected:
   Volume / Speed / Crop / Effects. The canvas stays on top and the timeline is collapsible, so the phone shows 5
   tools at a time instead of 18.
   *[UX pattern only]*
8. **Text overlay done minimally:**
   - "Aggiungi testo" creates one layer that **spans the whole clip by default**;
   - 3–4 hierarchy presets (Title / Heading / Body);
   - a **font carousel that renders the operator's own words**;
   - a background-pill option for legibility on busy AI footage.
   *[needs new engine capability: text rendering / compositing. Drawing through mediabunny `process` on a canvas
   is available]*
9. **Adjust sliders, each with a numeric box and its own reset, plus "Reset all".**
   Contrast / brightness / highlights / shadows / saturation / temperature / sharpen / blur, and 3 tone presets
   (B&W, darken, duotone). This fixes the washed-out or over-saturated look of generations, and the same panel
   applies to Aura's photo editor.
   *[needs new engine capability: per-frame colour pipeline, e.g. canvas filters in `process`; UX pattern only on
   the photo side]*
10. **Crop as a mode with Cancel / Done, a rule-of-thirds grid, and ratio chips named by destination**
    ("Storia", "Widescreen", not only "9:16"). Aura's crop already exists; this is the finishing layer.
    *[UX pattern only]*
11. **"Next action on this result" chaining.** After a download, Adobe lists "Converti in GIF / Ritaglia /
    Ridimensiona…" on the same file. Aura's version: after Save, offer "Also make 9:16", "Also make GIF" on the
    new asset, without re-uploading.
    *[UX pattern only]*
12. **Timeline visibility switch** (All layers / Selected layer / Scenes only). This is progressive disclosure: the
    timeline stays a simple strip until an overlay exists. It is worth having only once #8 lands.
    *[UX pattern only]*
13. **Background captions:** a language picker, "Genera", and "you can keep editing while it runs". This is useful
    for AI clips with TTS or voice.
    *[needs new engine capability: speech-to-text and text rendering]*
14. **Entrance animations as a short list** (Fade, Rise, Typewriter) split into Enter / Loop / Exit, with animated
    tiles for preview. Take the idea for text only, and keep it to 3–5 items.
    *[needs new engine capability: compositing / text rendering, as #8]*

### Scenes + templates (Adobe) versus multi-track (Clideo)

- **Adobe's model is a storyline of scenes.** A scene is a page with its own duration. Inside it, layers (video,
  text, images) carry their own timing, and one audio lane spans all scenes. Bulk actions live in Scene view
  ("mute all", "apply duration to all"). Precise layer timing is a secondary **All layers (Beta), desktop-only**
  view, and phone drops it entirely. Templates plug in as prebuilt scenes.
- **Clideo** runs a classic **multi-track** timeline, with video, images, text and audio on lanes against one global
  ruler. I did not tour it here; the parallel Clideo report has the measured detail.
- **For short AI clips (1–4 shots of 4–10 s each), scenes win.**
  - One generation equals one scene, which is how the operator thinks.
  - Nearly every operation is per-shot: trim, speed, fit, reorder, mute.
  - The rare overlay (title, caption) usually spans the whole shot.
  - It is the only model that still works on a phone.
- **Multi-track only pays off** for overlays or audio timed across cuts: a lower third spanning two shots, music
  ducked under speech.
- **Recommendation:** a scene strip as the primary surface, per-scene layer timing as an optional secondary view,
  and one audio lane under the strip. Skip templates: Aura's clips arrive already generated, so a template library
  adds weight without value.

---

## (c) Do not copy

- **Login wall after the work is done.** The page promises "Uso gratuito · Non è richiesta una carta di credito".
  A guest can edit everything, and only at Download (or Share) meets "Accedi o registrati gratuitamente per
  scaricare il tuo file" (`03-editor-desktop-38-download-progress-0.png`). Meanwhile the single-purpose quick
  actions **do** download without login, which is inconsistent.
- **Upload means consent and cloud storage.** "Caricando… accetti i Termini", and the file is already on Adobe's
  servers (`frictionlessUploadAssetId`) before the guest or sign-in choice. Aura should keep edits local and say so.
- **"Continue as guest" as a small text link** under four large SSO buttons.
- **Constant sign-up nags:**
  - a toast "Registrati o accedi per salvare il file" sitting over the canvas toolbar;
  - the header "Registrati per salvare";
  - a gradient "Inizia la prova gratuita" pill;
  - a red dot on "Scopri";
  - a permanent phone bottom bar "Per salvare il tuo file · Accedi".
- **Premium crowns mixed into free tiles**: Remove background, Enhance speech, Resize, font presets, text lockups.
  The paywall shows only on tap. Resizing, core for social, is 👑 in the editor but free as a quick action.
- **Overlay pile-up on first load**: cookie banner, sign-in modal and coach mark at once. The coach mark ("Ok")
  even blocks pointer events on the page. The cookie banner reappears on each origin (www, new.express, the
  quick-action iframe) and covers about 50% of a phone screen.
- **App-install interstitial on phone** in place of the uploader. Its "web" button is a deep link that goes nowhere.
- **Split that stacks layers** (two overlapping rows in one scene) instead of cutting into two scenes. The model is
  confusing, and the timeline grows a row per cut.
- **Silent bloat and misleading options.** The export offers **4K** for a 320×180 source. The trim quick action
  re-encoded an unchanged 4 s clip to 3.1× its size (164 KB to 512 KB). Aura should stream-copy when nothing
  changed and never offer upscales as "resolution".
- **Opaque failure.** Merge with clip + photo ended in "Si è verificato un problema… UUID", with no retry, no hint
  of which file failed, and Download greyed out.
- **Clutter:**
  - a 9-item rail plus flyouts plus an add-on marketplace (legal tools, GeoJSON charts) inside a video editor;
  - stock music rows with 20+ tags each;
  - the same action in 3–4 places: crop in the toolbar, the panel, "Riempi" and the quick action; volume in the
    toolbar and the panel; overlapping "…", layer and scene menus.
- **Localization sloppiness**: the temperature slider labelled "Reimposta", the Italian mega-menu in Portuguese,
  English items in the Italian UI ("No shape", "Highlighted text", "Close").
- **Rating and upsell furniture in the tool surface**: a floating "Valutaci" star on the player and an "Esplora
  altre azioni rapide" list competing with the primary action.
