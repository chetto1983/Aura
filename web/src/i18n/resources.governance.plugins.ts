// resources.governance.plugins.ts holds the plugins board copy (EN + IT), split out of
// resources.governance.ts to keep that file under the 600-LOC cap, as the skills and scheduler
// copy are. The two consts are spread into governanceEn.governance.plugins /
// governanceIt.governance.plugins.
//
// Add every key to BOTH languages: a key present in one and missing in the other is a
// defect, not a fallback.

export const pluginsEn = {
  installedHeading: 'Installed plugins',
  emptyHeading: 'No plugins installed',
  emptyBody: 'Review one above by its owner/repo/plugin name, then install it.',
  skills: 'Skills: {{names}}',
  connectors: 'Connectors: {{names}}',
  none: 'none',
  phase: {
    installing: 'Installing',
    removing: 'Removing',
  },
  removeAria: 'Remove {{name}}',
  removed: 'Removed {{name}}.',
  removeFailed: "Couldn't remove {{name}}.",
  removeStopped:
    'The removal of {{name}} stopped part way: {{reason}}. Removing it again finishes the job.',
  remove: {
    title: 'Remove {{name}}?',
    body: 'Its connectors leave every box and its skills are deleted; installing it again brings them back. A skill you changed since the install stays.',
    deleteData: 'Also delete its data (only in your box)',
    cancel: 'Cancel',
    confirm: 'Remove',
  },
  install: {
    heading: 'Install a plugin',
    refLabel: 'Plugin',
    refHint: 'owner/repo/plugin, or owner/repo for a repository that is one plugin.',
    review: 'Review',
    reviewing: 'Fetching the plugin…',
    reviewFailed: "Couldn't review this plugin.",
    submit: 'Install',
    installing:
      'Installing — each connector is verified first, up to two minutes for one in the box.',
    installFailed: "Couldn't install the plugin.",
    digestChanged: 'What this install would write changed since the review. Review it again.',
    reviewAgain: 'Review again',
    alreadyInstalled: '{{name}} is already installed. Remove it first to install it again.',
  },
  review: {
    by: 'by {{author}}',
    commit: 'Commit',
    digest: 'Review digest',
    skillsHeading: 'Skills',
    connectorsHeading: 'Connectors',
    skippedHeading: 'Skipped',
    notesHeading: 'Notes',
    skipped: '{{name}} — {{reason}}',
    placement: {
      box: 'in the box',
      remote: 'remote',
    },
    oauth: 'may ask for an authorization',
    mcpDisabled: 'Connectors are off: {{reason}}',
  },
  report: {
    heading: 'Install report',
    outcome: {
      ok: 'installed',
      skipped: 'skipped',
      failed: 'failed',
      attention: 'needs attention',
    },
    openServer: 'Open on the MCP board',
    stopped: 'The install stopped part way: {{reason}}',
  },
};

export const pluginsIt = {
  installedHeading: 'Plugin installati',
  emptyHeading: 'Nessun plugin installato',
  emptyBody: 'Esaminane uno qui sopra con il nome owner/repo/plugin, poi installalo.',
  skills: 'Competenze: {{names}}',
  connectors: 'Connettori: {{names}}',
  none: 'nessuno',
  phase: {
    installing: 'Installazione in corso',
    removing: 'Rimozione in corso',
  },
  removeAria: 'Rimuovi {{name}}',
  removed: '{{name}} rimosso.',
  removeFailed: 'Impossibile rimuovere {{name}}.',
  removeStopped:
    'La rimozione di {{name}} si è fermata a metà: {{reason}}. Rimuoverlo di nuovo completa il lavoro.',
  remove: {
    title: 'Rimuovere {{name}}?',
    body: "I suoi connettori escono da ogni box e le sue competenze vengono eliminate; installarlo di nuovo le riporta. Una competenza che hai modificato dopo l'installazione resta.",
    deleteData: 'Elimina anche i dati (solo nel tuo box)',
    cancel: 'Annulla',
    confirm: 'Rimuovi',
  },
  install: {
    heading: 'Installa un plugin',
    refLabel: 'Plugin',
    refHint: 'owner/repo/plugin, oppure owner/repo per un repository che è un solo plugin.',
    review: 'Esamina',
    reviewing: 'Recupero del plugin…',
    reviewFailed: 'Impossibile esaminare questo plugin.',
    submit: 'Installa',
    installing:
      'Installazione — ogni connettore viene verificato prima, fino a due minuti per uno nel box.',
    installFailed: 'Impossibile installare il plugin.',
    digestChanged:
      "Ciò che questa installazione scriverebbe è cambiato dopo l'esame. Esaminalo di nuovo.",
    reviewAgain: 'Esamina di nuovo',
    alreadyInstalled: '{{name}} è già installato. Rimuovilo prima di installarlo di nuovo.',
  },
  review: {
    by: 'di {{author}}',
    commit: 'Commit',
    digest: "Impronta dell'esame",
    skillsHeading: 'Competenze',
    connectorsHeading: 'Connettori',
    skippedHeading: 'Saltati',
    notesHeading: 'Note',
    skipped: '{{name}} — {{reason}}',
    placement: {
      box: 'nel box',
      remote: 'remoto',
    },
    oauth: "può chiedere un'autorizzazione",
    mcpDisabled: 'Connettori disattivati: {{reason}}',
  },
  report: {
    heading: "Esito dell'installazione",
    outcome: {
      ok: 'installato',
      skipped: 'saltato',
      failed: 'fallito',
      attention: 'richiede attenzione',
    },
    openServer: 'Apri nel board MCP',
    stopped: "L'installazione si è fermata a metà: {{reason}}",
  },
};
