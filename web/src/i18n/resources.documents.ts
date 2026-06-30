export const documentsEn = {
  documents: {
    loading: 'Loading documents...',
    loadingDetail: 'Loading document...',
    title: 'Document library',
    empty: 'No documents',
    filters: {
      search: 'Search documents',
      tag: 'Tag filter',
      scope: 'Scope',
    },
    scope: {
      all: 'All',
      library: 'Library',
      thread: 'Thread',
    },
    actions: {
      refresh: 'Refresh',
      search: 'Search',
      saveTags: 'Save tags',
      delete: 'Delete document',
      deletePermanently: 'Delete permanently',
      cancel: 'Cancel',
    },
    detail: {
      tags: 'Document tags',
      versions: 'Versions',
    },
    storage: {
      title: 'Storage orphans',
      bucket: 'Storage bucket',
      prefix: 'Object prefix',
      dryRun: 'Dry run',
      delete: 'Delete orphans',
      confirmLabel: 'Type DELETE ORPHAN OBJECTS to confirm',
      count_one: '{{count}} orphan',
      count_other: '{{count}} orphans',
      deleted_one: 'Deleted {{count}} orphan object',
      deleted_other: 'Deleted {{count}} orphan objects',
      none: 'No orphan objects',
      error: "Couldn't scan storage objects.",
    },
    delete: {
      title: 'Delete {{title}}',
      body: 'The catalog row, active graph entities, and future retrieval use will be removed.',
      confirmLabel: 'Type DELETE to confirm',
    },
    error: {
      list: "Couldn't load documents.",
      detail: "Couldn't load this document.",
    },
  },
} as const;

export const documentsIt = {
  documents: {
    loading: 'Caricamento documenti...',
    loadingDetail: 'Caricamento documento...',
    title: 'Libreria documenti',
    empty: 'Nessun documento',
    filters: {
      search: 'Cerca documenti',
      tag: 'Filtro tag',
      scope: 'Ambito',
    },
    scope: {
      all: 'Tutti',
      library: 'Libreria',
      thread: 'Thread',
    },
    actions: {
      refresh: 'Aggiorna',
      search: 'Cerca',
      saveTags: 'Salva tag',
      delete: 'Elimina documento',
      deletePermanently: 'Elimina definitivamente',
      cancel: 'Annulla',
    },
    detail: {
      tags: 'Tag documento',
      versions: 'Versioni',
    },
    storage: {
      title: 'Orfani storage',
      bucket: 'Bucket storage',
      prefix: 'Prefisso oggetti',
      dryRun: 'Simula',
      delete: 'Elimina orfani',
      confirmLabel: 'Scrivi DELETE ORPHAN OBJECTS per confermare',
      count_one: '{{count}} orfano',
      count_other: '{{count}} orfani',
      deleted_one: 'Eliminato {{count}} oggetto orfano',
      deleted_other: 'Eliminati {{count}} oggetti orfani',
      none: 'Nessun oggetto orfano',
      error: 'Impossibile analizzare gli oggetti storage.',
    },
    delete: {
      title: 'Elimina {{title}}',
      body: 'La riga catalogo, le entita grafo attive e gli usi futuri nel retrieval saranno rimossi.',
      confirmLabel: 'Scrivi DELETE per confermare',
    },
    error: {
      list: 'Impossibile caricare i documenti.',
      detail: 'Impossibile caricare questo documento.',
    },
  },
} as const;
