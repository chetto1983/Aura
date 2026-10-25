export const approvalEn = {
  approval: {
    badge: {
      aria_one: '{{count}} approval waiting',
      aria_other: '{{count}} approvals waiting',
      cleared: 'No approvals waiting.',
    },
    list: {
      label: 'Pending approvals',
      open: 'Open',
      empty: 'Nothing waiting on you.',
      untitled: 'Untitled',
    },
    kind: {
      clarification: 'Clarification',
      choice: 'Choice',
      approval: 'Approval',
    },
    frame: {
      approval: 'Approval required',
      input: 'Input required',
      review: 'Review the scope and consequence before continuing.',
    },
    lock: 'Answer the request above to continue.',
    question: {
      none: '(none)',
      gateway:
        'Approve {{tool}} (risk={{risk}})?\nThis mutating action is WITHHELD until you accept.\nargs: {{args}}',
      target: 'target: {{target}}',
      shell:
        'Approve the shell_exec command?\ndirectory: {{cwd}}\ncommand:\n{{command}}\nsha256: {{digest}}',
      scheduled: 'Approve scheduled {{kind}} task {{task}}? It will not run until you approve.',
      scheduledDetailed:
        'Approve scheduled {{kind}} task {{task}} ({{schedule}}, risk={{risk}})? It will not run until you approve.',
      plugin: {
        install:
          'Approve plugin install?\nplugin: {{plugin}}\nsource: {{source}}\nskills {{skills}}\nconnectors {{connectors}}\nskipped {{skipped}}\nmcp: {{mcp}}\nnotes {{notes}}\nsha256: {{digest}}',
        remove:
          'Approve plugin removal?\nplugin: {{plugin}}\nsource: {{source}}\nskills {{skills}}\nconnectors {{connectors}}\nplugin data: {{data}}\nsha256: {{digest}}',
        none: 'none',
        mcpEnabled: 'enabled',
        mcpDisabled: 'disabled — {{reason}}',
        dataKept: 'kept',
        dataDeleted: 'deleted in your box',
      },
    },
    // The gateway's approval scopes (amendment #127). It ships a stable code and an
    // English fallback; these are the words the cockpit puts on the buttons.
    scope: {
      once: 'Approve once',
      session: 'Approve {{subject}} for this conversation',
      always: 'Always approve {{subject}}',
    },
    card: {
      freeText: 'Your answer',
      freeTextPlaceholder: 'Type your answer',
      answer: 'Answer',
      decline: 'Decline',
      cancel: 'Cancel run',
      declined: 'Declined.',
      answered: 'Answered.',
      cancelled: 'Run cancelled.',
      confirmCancel: 'Stop this run?',
      confirmCancelYes: 'Stop run',
      confirmCancelNo: 'Keep running',
      error: "Couldn't resume this run. It may have already been answered or cancelled.",
    },
    terminal: {
      expired: 'Expired — auto-resolved.',
    },
  },
};

export const approvalIt = {
  approval: {
    badge: {
      aria_one: '{{count}} approvazione in attesa',
      aria_other: '{{count}} approvazioni in attesa',
      cleared: 'Nessuna approvazione in attesa.',
    },
    list: {
      label: 'Approvazioni in attesa',
      open: 'Apri',
      empty: 'Niente che richieda la tua attenzione.',
      untitled: 'Senza titolo',
    },
    kind: {
      clarification: 'Chiarimento',
      choice: 'Scelta',
      approval: 'Approvazione',
    },
    frame: {
      approval: 'Approvazione richiesta',
      input: 'Input richiesto',
      review: 'Controlla ambito e conseguenze prima di continuare.',
    },
    lock: 'Rispondi alla richiesta qui sopra per continuare.',
    question: {
      none: '(nessuno)',
      gateway:
        'Approva {{tool}} (rischio={{risk}})?\nQuesta azione mutante resta BLOCCATA finché non accetti.\nargomenti: {{args}}',
      target: 'oggetto: {{target}}',
      shell:
        'Approva il comando shell_exec?\ndirectory: {{cwd}}\ncomando:\n{{command}}\nsha256: {{digest}}',
      scheduled:
        "Approva l'attività pianificata {{kind}} {{task}}? Non verrà eseguita finché non approvi.",
      scheduledDetailed:
        "Approva l'attività pianificata {{kind}} {{task}} ({{schedule}}, rischio={{risk}})? Non verrà eseguita finché non approvi.",
      plugin: {
        install:
          "Approva l'installazione del plugin?\nplugin: {{plugin}}\norigine: {{source}}\ncompetenze {{skills}}\nconnettori {{connectors}}\nsaltati {{skipped}}\nmcp: {{mcp}}\nnote {{notes}}\nsha256: {{digest}}",
        remove:
          'Approva la rimozione del plugin?\nplugin: {{plugin}}\norigine: {{source}}\ncompetenze {{skills}}\nconnettori {{connectors}}\ndati del plugin: {{data}}\nsha256: {{digest}}',
        none: 'nessuno',
        mcpEnabled: 'attivo',
        mcpDisabled: 'disattivato — {{reason}}',
        dataKept: 'conservati',
        dataDeleted: 'eliminati nel tuo box',
      },
    },
    scope: {
      once: 'Approva una volta',
      session: 'Approva {{subject}} per questa conversazione',
      always: 'Approva sempre {{subject}}',
    },
    card: {
      freeText: 'La tua risposta',
      freeTextPlaceholder: 'Scrivi la tua risposta',
      answer: 'Rispondi',
      decline: 'Rifiuta',
      cancel: 'Annulla esecuzione',
      declined: 'Rifiutata.',
      answered: 'Risposta inviata.',
      cancelled: 'Esecuzione annullata.',
      confirmCancel: 'Fermare questa esecuzione?',
      confirmCancelYes: 'Ferma esecuzione',
      confirmCancelNo: 'Continua',
      error:
        'Impossibile riprendere questa esecuzione. Potrebbe essere già stata gestita o annullata.',
    },
    terminal: {
      expired: 'Scaduta — risolta automaticamente.',
    },
  },
};
