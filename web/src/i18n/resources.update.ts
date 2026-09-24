export const updateEn = {
  update: {
    indicator: {
      pending: 'Update available',
      failed: 'Update failed',
    },
    dialog: {
      title: 'Update available',
      version: 'Version',
      built: 'Built',
      waiting: 'Waiting for',
      deadline: 'It will install itself at the first quiet moment, by {{time}} {{day}}.',
      firstPause: 'It will install itself at the first quiet moment.',
      mandatory: 'Mandatory update: it will start at the first quiet moment',
      deferred: 'Postponed: no restart before {{time}} {{day}}.',
      impact: {
        label: 'Right now',
        running_one: '{{count}} task in progress',
        running_other: '{{count}} tasks in progress',
        recent: 'last activity {{ago}}',
        idle: 'nobody is using Aura',
      },
      failed: 'The last attempt did not succeed',
      apply: 'Update now',
      retry: 'Retry',
      later: 'Later',
      close: 'Close',
      defer: {
        label: 'Postpone',
        hour: '1 hour',
        fourHours: '4 hours',
        tonight: 'Until tonight',
      },
      errors: {
        conflict: 'The update changed state in the meantime; this is where it stands now.',
        forbidden: 'Only an administrator can decide on updates.',
        invalid: 'That time is no longer valid. Choose again.',
        generic: 'Aura could not record the choice. Try again.',
      },
    },
    deferred: {
      title: 'Update postponed',
      body: 'No restart before {{time}} {{day}}.',
      clamped: 'That is as far as it can go: the update has to be installed by then.',
      ok: 'OK',
    },
    banner: 'Aura is updating in a few seconds: save what you are writing',
    overlay: {
      title: 'Aura is updating…',
      body: 'Back in about 2 minutes',
    },
  },
};

export const updateIt = {
  update: {
    indicator: {
      pending: 'Aggiornamento disponibile',
      failed: 'Aggiornamento non riuscito',
    },
    dialog: {
      title: 'Aggiornamento disponibile',
      version: 'Versione',
      built: 'Compilata',
      waiting: 'In attesa da',
      deadline: 'Si installerà da solo alla prima pausa entro {{day}} alle {{time}}.',
      firstPause: 'Si installerà da solo alla prima pausa.',
      mandatory: 'Aggiornamento obbligatorio: partirà alla prima pausa',
      deferred: 'Rimandato: nessun riavvio prima di {{day}} alle {{time}}.',
      impact: {
        label: 'In questo momento',
        running_one: '{{count}} attività in corso',
        running_other: '{{count}} attività in corso',
        recent: 'ultima attività {{ago}}',
        idle: 'nessuno sta usando Aura',
      },
      failed: "L'ultimo tentativo non è riuscito",
      apply: 'Aggiorna subito',
      retry: 'Riprova',
      later: 'Più tardi',
      close: 'Chiudi',
      defer: {
        label: 'Rimanda',
        hour: '1 ora',
        fourHours: '4 ore',
        tonight: 'Fino a stanotte',
      },
      errors: {
        conflict: "Nel frattempo l'aggiornamento ha cambiato stato: ecco com'è adesso.",
        forbidden: 'Solo un amministratore può decidere sugli aggiornamenti.',
        invalid: "Quell'orario non è più valido. Scegli di nuovo.",
        generic: 'Aura non è riuscita a registrare la scelta. Riprova.',
      },
    },
    deferred: {
      title: 'Aggiornamento rimandato',
      body: 'Nessun riavvio prima di {{day}} alle {{time}}.',
      clamped: "Più in là non si può: entro quell'ora l'aggiornamento va installato.",
      ok: 'Va bene',
    },
    banner: 'Aura si aggiorna tra pochi secondi: salva quello che stai scrivendo',
    overlay: {
      title: 'Aura si sta aggiornando…',
      body: 'Torna disponibile in circa 2 minuti',
    },
  },
};
