import { en as coreEn, it as coreIt } from '@svar-ui/core-locales';

// SVAR ships Italian for the CORE words (calendar, date formats, ok/cancel) but only
// English and Chinese for the file manager and the uploader, so those two dictionaries are
// written here. The keys ARE the English strings -- that is the component's lookup contract,
// not a naming choice -- so they are reproduced verbatim from
// @svar-ui/filemanager-locales/locales/en.js and @svar-ui/uploader-locales/locales/en.js.
// A key that drifts from upstream silently falls back to English rather than failing.
const filemanagerIt = {
  'My files': 'I miei file',

  'Add New': 'Aggiungi',
  Ok: 'Ok',
  Cancel: 'Annulla',

  'Delete files': 'Elimina file',
  Files: 'File',
  Folder: 'Cartella',
  'Back to parent folder': 'Torna alla cartella superiore',

  Information: 'Informazioni',
  Found: 'Trovati',

  'Are you sure you want to delete these items:': 'Vuoi davvero eliminare questi elementi:',
  'Enter file name': 'Nome del file',
  'Enter folder name': 'Nome della cartella',

  Result: 'Risultato',
  Search: 'Cerca',
  'Search results in': 'Risultati della ricerca in',
  'Add new file': 'Nuovo file',
  'Add new folder': 'Nuova cartella',
  'Upload file': 'Carica un file',

  Size: 'Dimensione',
  Name: 'Nome',
  Type: 'Tipo',
  Date: 'Data',

  Rename: 'Rinomina',
  Cut: 'Taglia',
  Delete: 'Elimina',
  Copy: 'Copia',
  Paste: 'Incolla',
  Download: 'Scarica',
  Count: 'Numero',
  folder: 'cartella',
  folders: 'cartelle',
  file: 'file',
  files: 'file',
  'Multiple files': 'Più file',
  multiple: 'multiplo',
  Preview: 'Anteprima',
  of: 'di',
  used: 'usati',
  'Select file or folder to view details':
    'Seleziona un file o una cartella per vederne i dettagli',
  'Looks like nothing is here': 'Qui non c’è ancora niente',
  'Unknown file': 'File sconosciuto',

  'A miniature file preview': 'Anteprima in miniatura del file',
};

const uploaderIt = {
  B: 'B',
  KB: 'KB',
  MB: 'MB',
  GB: 'GB',
  TB: 'TB',
  PB: 'PB',
  EB: 'EB',
  'Drop files here or': 'Trascina qui i file oppure',
  'select files': 'scegli i file',
};

/** The word pack for one cockpit language. English is the component's own default. */
export function filesWords(language: string): Record<string, unknown> {
  if (!language.toLowerCase().startsWith('it')) return { ...coreEn };
  return { ...coreIt, filemanager: filemanagerIt, uploader: uploaderIt };
}
