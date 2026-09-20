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
const PALETTE_TOKENS: Readonly<Record<string, string>> = {
  'accent-primary': '--color-on-accent',
  'accent-primary-hover': '--color-on-accent',
  'accent-primary-active': '--color-on-accent',
  'accent-stateless': '--color-on-accent',
  'bg-stateless': '--color-surface-3',
  'bg-active': '--color-surface-3',
  'bg-hover': '--color-surface-3',
  'bg-primary': '--color-surface',
  'bg-secondary': '--color-surface-2',
  'bg-primary-active': '--color-accent',
  'txt-primary': '--color-text',
  'txt-secondary': '--color-text-muted',
  'txt-placeholder': '--color-text-faint',
  'borders-primary': '--color-border',
  'borders-secondary': '--color-border-strong',
  'borders-button': '--color-border-strong',
  'borders-item': '--color-border',
  'borders-disabled': '--color-text-disabled',
  'icon-primary': '--color-text-muted',
  'icons-primary-hover': '--color-text',
  'icons-secondary': '--color-text-faint',
  'icons-secondary-hover': '--color-text-muted',
  'icons-invert': '--color-accent',
  'btn-primary-text': '--color-accent',
  'btn-secondary-text': '--color-text',
  'btn-disabled-text': '--color-text-disabled',
  'link-primary': '--color-accent-text',
  'link-stateless': '--color-accent-text',
  'link-hover': '--color-text',
  'link-active': '--color-accent-text',
};

export function filerobotTheme(root: Element = document.documentElement): FilerobotTheme {
  const style = getComputedStyle(root);
  const palette: Record<string, string> = {};
  for (const [key, token] of Object.entries(PALETTE_TOKENS)) {
    const value = style.getPropertyValue(token).trim();
    if (value !== '') palette[key] = value;
  }
  const font = style.getPropertyValue('--font-sans').trim();
  return { palette, ...(font === '' ? {} : { typography: { fontFamily: font } }) };
}

/** styled-components 6 stopped filtering unknown props, and Filerobot's styled components pass
 *  theirs (showTabsDrawer, isPhoneScreen, active…) straight to the DOM: about 35 React warnings
 *  per mount in spike 105. This restores the v5 filter for DOM targets only. */
export function forwardDomProp(prop: string, target: unknown): boolean {
  return typeof target === 'string' ? isPropValid(prop) : true;
}
