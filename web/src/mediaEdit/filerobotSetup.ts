import isPropValid from '@emotion/is-prop-valid';

// filerobotSetup.ts — everything Aura hands Filerobot that is not a prop value: the Italian
// table, the theme read from Aura's tokens, and the prop filter styled-components 6 needs.
//
// The Italian table is Aura's own. Filerobot can fetch translations from Scaleflex's service
// (useBackendTranslations, on by default), which sends a request off-origin and, without a
// Scaleflex grid id, returns nothing (spike 105). The editor always runs with it off.

// Filerobot types its `theme` prop as `ThemeOverride` from '@scaleflex/ui/theme', an entry
// that does not export it, so the prop is `any`. This is the part of that override Aura fills.
export interface FilerobotTheme {
  readonly palette: Readonly<Record<string, string>>;
  readonly typography?: { readonly fontFamily: string };
}

export const FILEROBOT_IT: Readonly<Record<string, string>> = {
  name: 'Nome',
  save: 'Salva',
  saveAs: 'Salva come',
  back: 'Indietro',
  loading: 'Caricamento…',
  resetOperations: 'Annulla tutte le modifiche',
  changesLoseWarningHint: 'Se premi “reimposta” perderai le modifiche. Vuoi continuare?',
  discardChangesWarningHint: "Se chiudi la finestra, l'ultima modifica non verrà salvata.",
  cancel: 'Annulla',
  apply: 'Applica',
  warning: 'Attenzione',
  confirm: 'Conferma',
  discardChanges: 'Scarta le modifiche',
  undoTitle: "Annulla l'ultima operazione",
  redoTitle: "Ripeti l'ultima operazione",
  showImageTitle: "Mostra l'immagine originale",
  zoomInTitle: 'Ingrandisci',
  fitTitle: 'Adatta',
  zoomOutTitle: 'Riduci',
  toggleZoomMenuTitle: 'Menu dello zoom',
  adjustTab: 'Regola',
  finetuneTab: 'Ritocca',
  filtersTab: 'Filtri',
  watermarkTab: 'Filigrana',
  annotateTabLabel: 'Annota',
  resize: 'Ridimensiona',
  resizeTab: 'Ridimensiona',
  imageName: "Nome dell'immagine",
  invalidImageError: 'Immagine non valida.',
  uploadImageError: "Errore durante il caricamento dell'immagine.",
  areNotImages: 'non sono immagini',
  isNotImage: "non è un'immagine",
  toBeUploaded: 'da caricare',
  cropTool: 'Ritaglia',
  original: 'Originale',
  custom: 'Personalizzato',
  square: 'Quadrato',
  landscape: 'Orizzontale',
  portrait: 'Verticale',
  ellipse: 'Ellisse',
  classicTv: 'TV classica',
  cinemascope: 'Cinemascope',
  arrowTool: 'Freccia',
  blurTool: 'Sfocatura',
  brightnessTool: 'Luminosità',
  contrastTool: 'Contrasto',
  ellipseTool: 'Ellisse',
  unFlipX: 'Annulla specchio orizzontale',
  flipX: 'Specchia in orizzontale',
  unFlipY: 'Annulla specchio verticale',
  flipY: 'Specchia in verticale',
  hsvTool: 'HSV',
  hue: 'Tonalità',
  brightness: 'Luminosità',
  saturation: 'Saturazione',
  value: 'Valore',
  imageTool: 'Immagine',
  importing: 'Importazione…',
  addImage: 'Aggiungi immagine',
  uploadImage: 'Carica immagine',
  fromGallery: 'Dalla galleria',
  lineTool: 'Linea',
  penTool: 'Penna',
  polygonTool: 'Poligono',
  sides: 'Lati',
  rectangleTool: 'Rettangolo',
  cornerRadius: 'Raggio degli angoli',
  resizeWidthTitle: 'Larghezza in pixel',
  resizeHeightTitle: 'Altezza in pixel',
  toggleRatioLockTitle: 'Blocca le proporzioni',
  resetSize: 'Ripristina la dimensione originale',
  rotateTool: 'Ruota',
  textTool: 'Testo',
  textSpacings: 'Spaziatura del testo',
  textAlignment: 'Allineamento del testo',
  fontFamily: 'Carattere',
  size: 'Dimensione',
  letterSpacing: 'Spaziatura delle lettere',
  lineHeight: 'Interlinea',
  warmthTool: 'Calore',
  addWatermark: 'Aggiungi filigrana',
  addTextWatermark: 'Aggiungi filigrana di testo',
  addWatermarkTitle: 'Scegli il tipo di filigrana',
  uploadWatermark: 'Carica filigrana',
  addWatermarkAsText: 'Aggiungi come testo',
  padding: 'Margine',
  paddings: 'Margini',
  shadow: 'Ombra',
  horizontal: 'Orizzontale',
  vertical: 'Verticale',
  blur: 'Sfocatura',
  opacity: 'Opacità',
  transparency: 'Trasparenza',
  position: 'Posizione',
  stroke: 'Contorno',
  saveAsModalTitle: 'Salva come',
  extension: 'Estensione',
  format: 'Formato',
  nameIsRequired: 'Il nome è obbligatorio.',
  quality: 'Qualità',
  imageDimensionsHoverTitle: "Dimensione dell'immagine salvata (larghezza × altezza)",
  cropSizeLowerThanResizedWarning:
    "Nota: l'area di ritaglio è più piccola del ridimensionamento applicato e la qualità potrebbe peggiorare",
  actualSize: 'Dimensione reale (100%)',
  fitSize: 'Adatta alla finestra',
  addImageTitle: "Seleziona l'immagine da aggiungere…",
  mutualizedFailedToLoadImg: "Impossibile caricare l'immagine.",
  tabsMenu: 'Menu',
  download: 'Scarica',
  width: 'Larghezza',
  height: 'Altezza',
  cropItemNoEffect: 'Anteprima non disponibile per questo formato',
  px: 'px',
  invalidTextContent: 'Testo non valido',
  baselineShift: 'Spostamento della linea di base',
  aiTab: 'Strumenti AI',
  objectRemovalTool: 'Rimozione di oggetti',
  objectRemovalBrushSize: 'Dimensione del pennello (px)',
  objectRemovalApplyButton: 'Applica',
  objectRemovalBrushMode: 'Modalità del pennello',
  objectRemovalBrushCircleType: 'Pennello rotondo',
  objectRemovalBrushSquareType: 'Pennello quadrato',
  objectRemovalApplyingText: "Rimozione dell'area selezionata",
  objectRemovalMarkModeTooltip: 'Segna',
  objectRemovalUnMarkModeTooltip: 'Togli il segno',
  objectRemovalCancelConfirmationTitle: "Annulla l'operazione",
  objectRemovalCancelConfirmationHint: "Vuoi davvero annullare la rimozione dell'oggetto da",
  objectRemovalCancelConfirmationHintCompletion: 'questa risorsa?',
  theKeyword: 'la',
};

/** Aura's table for Italian; undefined lets Filerobot use its own English. Filter names
 *  (Clarendon, Sepia…) are raw labels in Filerobot and stay English whatever this holds. */
export function filerobotTranslations(
  language: string,
): Readonly<Record<string, string>> | undefined {
  return language.toLowerCase().startsWith('it') ? FILEROBOT_IT : undefined;
}

// Scaleflex palette key → Aura token. The editor reads the tokens once, when it opens.
//
// Every key a Scaleflex or Filerobot component reads and this map does not answer keeps the
// value from `lightPalette` (@scaleflex/ui theme-provider merges the override ON TOP of it),
// so a gap here is not a missing accent: it is a light-theme colour painted into Aura's dark
// cockpit. The keys below are the ones the photo editor actually reaches, found by reading
// the two packages rather than by trying colours; the brand identities (`instagram`,
// `dropbox`, `indigo`…) are deliberately left to Scaleflex.
const PALETTE_TOKENS: Readonly<Record<string, string>> = {
  'txt-primary': '--color-text',
  'txt-secondary': '--color-text-muted',
  'txt-secondary-invert': '--color-text',
  'txt-placeholder': '--color-text-faint',
  'txt-warning': '--color-warning',
  'txt-error': '--color-danger',
  'txt-info': '--color-info',
  // `accent-stateless` is a primary button's BACKGROUND and `btn-primary-text` its label
  // (core/button/button.mixin.js), while `accent-primary` is a foreground: link text, a
  // focused field's border, the crop handle's stroke. Aura's accent splits into a surface
  // (--color-accent) and its foreground (--color-on-accent), and only the foreground stays
  // legible in both roles, so that is the one both keys take.
  'accent-primary': '--color-on-accent',
  'accent-stateless': '--color-on-accent',
  // `accent-primary-active` carries the same double duty: Filerobot paints the SELECTED tab's
  // label and icon with it over `bg-primary-active` (Tabs.styled.js), while @scaleflex/ui's
  // button mixin uses it as a pressed background under `btn-primary-text`. A colour derived
  // between the ring and the text read as a washed-out label on the chosen tab, so it takes
  // the accent's foreground too — which is also what `btn-primary-text` inverts against.
  'accent-primary-active': '--color-on-accent',
  'accent-primary-hover': '--color-ring',
  'accent-primary-disabled': '--color-surface-3',
  'bg-primary': '--color-surface',
  'bg-primary-hover': '--color-surface-3',
  'bg-primary-active': '--color-accent',
  'bg-primary-stateless': '--color-surface-3',
  'bg-secondary': '--color-surface-2',
  'bg-stateless': '--color-surface-3',
  // The chosen row of a menu — the crop ratio the image is on. Sharing `bg-stateless`'s
  // surface left it indistinguishable from the menu behind it; the accent makes it read as
  // chosen, like the selected tab, which is also `bg-primary-active`.
  'bg-active': '--color-accent',
  'bg-hover': '--color-surface-3',
  'bg-grey': '--color-surface-3',
  'icon-primary': '--color-text-muted',
  // Filerobot's global stylesheet colours every non-button svg through the PLURAL spelling,
  // which is not in Scaleflex's own Color enum: unanswered, the declaration was dropped and
  // those icons fell back to whatever text colour they inherited.
  'icons-primary': '--color-text-muted',
  'icons-primary-hover': '--color-text',
  'icons-secondary': '--color-text-faint',
  'icons-secondary-hover': '--color-text-muted',
  'icons-muted': '--color-text-faint',
  'icons-placeholder': '--color-border',
  'icons-invert': '--color-accent',
  'btn-primary-text': '--color-accent',
  'btn-secondary-text': '--color-text',
  'btn-disabled-text': '--color-text-disabled',
  'link-primary': '--color-accent-text',
  'link-stateless': '--color-accent-text',
  'link-hover': '--color-text',
  'link-active': '--color-accent-text',
  'borders-primary': '--color-border',
  'borders-secondary': '--color-border',
  'borders-item': '--color-border',
  'borders-strong': '--color-border-strong',
  'borders-button': '--color-border-strong',
  'borders-primary-hover': '--color-border-strong',
  'border-primary-stateless': '--color-border-strong',
  'border-active-bottom': '--color-on-accent',
  'borders-disabled': '--color-text-disabled',
  error: '--color-danger',
  warning: '--color-warning',
  success: '--color-success',
  info: '--color-info',
  modified: '--color-info',
  'active-secondary': '--color-surface',
  // Filerobot asks the palette for "access-primary" when it fills the crop and transform
  // handles (components/Layers/TransformersLayer/CropTransformer.js). The key is a typo for
  // `accent-primary` and exists nowhere in Scaleflex's enum, so the handles fell through to
  // Konva's own white; on a light canvas that is a white dot on a white photo.
  'access-primary': '--color-surface',
};

/** Keys Scaleflex fills with a tint rather than a colour: an Aura token at an opacity. */
const PALETTE_ALPHA: Readonly<Record<string, readonly [string, number]>> = {
  accent_1_2_opacity: ['--color-on-accent', 0.12],
  accent_1_8_opacity: ['--color-on-accent', 0.18],
  accent_2_8_opacity: ['--color-on-accent', 0.28],
  accent_4_0_opacity: ['--color-on-accent', 0.4],
  'accent-stateless_0_4_opacity': ['--color-on-accent', 0.4],
  'border-hover-bottom': ['--color-on-accent', 0.18],
  'bg-blue': ['--color-info', 0.14],
  'bg-green': ['--color-success', 0.14],
  'bg-orange': ['--color-warning', 0.14],
  'bg-red': ['--color-danger', 0.14],
  'bg-red-light': ['--color-danger', 0.08],
  'white-0-7-8-overlay': ['--color-surface', 0.78],
};

/** Shadows and the modal scrim. These are black alphas in Aura's light theme as well as its
 *  dark one (--shadow-popover, the cockpit's own dialog overlay), so they are not tokens. */
const PALETTE_LITERALS: Readonly<Record<string, string>> = {
  'light-shadow': 'rgb(0 0 0 / 0.18)',
  'medium-shadow': 'rgb(0 0 0 / 0.24)',
  'large-shadow': 'rgb(0 0 0 / 0.28)',
  'extra-0-3-overlay': 'rgb(0 0 0 / 0.6)',
};

/** The three channels of `#rrggbb`. Aura's generated tokens are all hex; anything else is a
 *  token this file has no arithmetic for, and its key is left to Scaleflex rather than
 *  guessed at. */
function channels(value: string): readonly [number, number, number] | undefined {
  if (!/^#[0-9a-f]{6}$/i.test(value)) return undefined;
  const packed = Number.parseInt(value.slice(1), 16);
  return [(packed >> 16) & 255, (packed >> 8) & 255, packed & 255];
}

function withAlpha(value: string, alpha: number): string | undefined {
  const rgb = channels(value);
  return rgb === undefined ? undefined : `rgb(${rgb.join(' ')} / ${String(alpha)})`;
}

export function filerobotTheme(root: Element = document.documentElement): FilerobotTheme {
  const style = getComputedStyle(root);
  const token = (name: string): string => style.getPropertyValue(name).trim();
  const palette: Record<string, string> = { ...PALETTE_LITERALS };
  for (const [key, name] of Object.entries(PALETTE_TOKENS)) {
    const value = token(name);
    if (value !== '') palette[key] = value;
  }
  for (const [key, [name, alpha]] of Object.entries(PALETTE_ALPHA)) {
    const value = withAlpha(token(name), alpha);
    if (value !== undefined) palette[key] = value;
  }
  const font = token('--font-sans');
  return { palette, ...(font === '' ? {} : { typography: { fontFamily: font } }) };
}

/** styled-components 6 stopped filtering unknown props, and Filerobot's styled components pass
 *  theirs (showTabsDrawer, isPhoneScreen, active…) straight to the DOM: about 35 React warnings
 *  per mount in spike 105. This restores the v5 filter for DOM targets only. */
export function forwardDomProp(prop: string, target: unknown): boolean {
  return typeof target === 'string' ? isPropValid(prop) : true;
}
