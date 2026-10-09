import { en as coreEn, it as coreIt } from '@svar-ui/core-locales';
import { en as kanbanEn, it as kanbanIt } from '@svar-ui/kanban-locales';

// SVAR ships Italian for the core and the Kanban words but only English and Chinese for the
// editor, so the editor's Italian is written here. The keys ARE the English strings, the
// component's lookup contract, reproduced verbatim from @svar-ui/editor-locales/locales/en.js;
// a key that drifts from upstream silently falls back to English.
const editorIt = {
  editor: {
    'This field is required': 'Campo obbligatorio',
    'Invalid value': 'Valore non valido',
    Yes: 'Sì',
    No: 'No',
    Save: 'Salva',
    Cancel: 'Annulla',
    'No data': 'Nessun dato',
  },
};

/** The words for the board's Locale: the core, the Kanban and the editor groups. */
export function boardWords(language: string) {
  return language.startsWith('it')
    ? { ...coreIt, ...kanbanIt, ...editorIt }
    : { ...coreEn, ...kanbanEn };
}
