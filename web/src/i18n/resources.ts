export const resources = {
  en: {
    translation: {
      language: {
        switcherLabel: 'Language',
        english: 'English',
        italian: 'Italiano',
      },
      login: {
        title: 'Aura',
        subtitle: 'Sign in to continue',
        fieldLabel: 'Operator passphrase',
        fieldHint: 'Required when Aura is exposed beyond loopback.',
        cta: 'Sign in',
        ctaInFlight: 'Signing in...',
        errors: {
          wrongPassphrase: "That passphrase didn't match. Check it and try again.",
          notConfigured:
            "Sign-in isn't available - this Aura instance has no operator passphrase configured.",
          network: "Couldn't reach Aura. Check the server is running and try again.",
        },
        sessionExpired: 'Your session expired. Sign in again to continue.',
        showPassword: 'Show passphrase',
        hidePassword: 'Hide passphrase',
      },
      shell: {
        primaryNav: 'Primary',
        navigation: 'Navigation',
        displayWorkspace: 'Display workspace',
        chatRegion: 'Chat',
        modes: {
          chat: 'Chat',
          tree: 'Tree',
          graph: 'Graph',
          displays: 'Displays',
          settings: 'Settings',
        },
      },
      chat: {
        composer: {
          placeholder: 'Ask Aura',
          send: 'Send',
          sendAria: 'Send message',
          stop: 'Stop',
          stopAria: 'Stop the current response',
        },
        running: 'Running...',
        empty: {
          thread: {
            heading: 'Ask Aura',
            body: 'Type a prompt below to start this run.',
          },
        },
        error: {
          stream:
            'The response stopped unexpectedly. Retry the last message or check the runtime status.',
        },
        reasoning: {
          show: 'Show reasoning',
          hide: 'Hide reasoning',
        },
        tool: {
          showRaw: 'Show raw result',
          hideRaw: 'Hide raw result',
          status: {
            running: 'Running',
            done: 'Done',
            error: 'Error',
          },
        },
      },
      conversations: {
        heading: 'Conversations',
        loading: 'Loading conversations...',
        loadError: "Couldn't load conversations. Refresh the list.",
        untitled: 'Untitled',
        includeArchived: 'Show archived',
        archivedTag: 'Archived',
        renameLabel: 'Conversation title',
        empty: {
          heading: 'Start a run',
          body: 'Ask Aura a question to begin. Your conversations show up here.',
        },
        actions: {
          rename: 'Rename',
          archive: 'Archive',
          unarchive: 'Unarchive',
          delete: 'Delete permanently',
        },
        delete: {
          title: 'Delete conversation?',
          body: 'This permanently deletes "{{title}}" and its messages. This can\'t be undone.',
          confirm: 'Delete permanently',
          cancel: 'Keep conversation',
        },
        search: {
          label: 'Search conversations',
          placeholder: 'Search conversations',
          searching: 'Searching...',
          empty: {
            heading: 'No matches',
            body: 'No conversations contain "{{query}}". Try a different term.',
          },
        },
      },
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
        card: {
          freeText: 'Your answer',
          freeTextPlaceholder: 'Type your answer',
          answer: 'Answer',
          decline: 'Decline',
          cancel: 'Cancel run',
          declined: 'The agent will continue, informed you declined.',
          answered: 'Answered — run resumed.',
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
      footer: {
        tokens: 'Tokens',
        cache: 'Cache',
        cost: 'Cost',
        context: 'Context',
        perTurn: 'This turn',
        session: 'Session',
        none: '—',
        gaugeValue: '{{used}} / {{window}} · {{percent}}%',
        contextLabel: 'Context budget',
        compacted: 'Compacted {{count}} older turns',
      },
      skeleton: {
        shell: 'Loading cockpit...',
        login: 'Loading sign-in...',
        page: 'Loading page...',
      },
      health: {
        title: 'Runtime',
        loading: 'Checking runtime...',
        unreachable:
          "Can't read runtime status. The health endpoints aren't responding - the server may be starting or down.",
        labels: {
          liveness: 'Liveness',
          readiness: 'Readiness',
          postgres: 'Postgres',
          neo4j: 'Neo4j',
          bindAddress: 'Bind address',
          build: 'Build',
        },
        status: {
          unavailable: 'Unavailable',
          live: 'Live',
          ready: 'Ready',
          degraded: 'Degraded',
          unknown: 'Unknown',
        },
        lastChecked: 'Last checked {{time}}',
        relative: {
          never: 'never',
          justNow: 'just now',
          secondsAgo: '{{count}}s ago',
          minutesAgo: '{{count}}m ago',
        },
      },
      notFound: {
        title: 'Page not found',
        beforeLink: "That cockpit page doesn't exist.",
        link: 'Return to dashboard',
      },
      errorBoundary: {
        title: 'Aura could not render this view.',
        action: 'Reload to try again.',
      },
    },
  },
  it: {
    translation: {
      language: {
        switcherLabel: 'Lingua',
        english: 'English',
        italian: 'Italiano',
      },
      login: {
        title: 'Aura',
        subtitle: 'Accedi per continuare',
        fieldLabel: 'Frase segreta operatore',
        fieldHint: 'Necessaria quando Aura è esposta oltre il loopback.',
        cta: 'Accedi',
        ctaInFlight: 'Accesso in corso...',
        errors: {
          wrongPassphrase: 'La frase segreta non corrisponde. Controllala e riprova.',
          notConfigured:
            "L'accesso non è disponibile: questa istanza Aura non ha una frase segreta operatore configurata.",
          network:
            'Impossibile raggiungere Aura. Verifica che il server sia in esecuzione e riprova.',
        },
        sessionExpired: 'La sessione è scaduta. Accedi di nuovo per continuare.',
        showPassword: 'Mostra frase segreta',
        hidePassword: 'Nascondi frase segreta',
      },
      shell: {
        primaryNav: 'Principale',
        navigation: 'Navigazione',
        displayWorkspace: 'Area display',
        chatRegion: 'Area chat',
        modes: {
          chat: 'Chat',
          tree: 'Albero',
          graph: 'Grafo',
          displays: 'Display',
          settings: 'Impostazioni',
        },
      },
      chat: {
        composer: {
          placeholder: 'Chiedi ad Aura',
          send: 'Invia',
          sendAria: 'Invia messaggio',
          stop: 'Ferma',
          stopAria: 'Ferma la risposta in corso',
        },
        running: 'In esecuzione...',
        empty: {
          thread: {
            heading: 'Chiedi ad Aura',
            body: 'Scrivi un prompt qui sotto per avviare questa esecuzione.',
          },
        },
        error: {
          stream:
            "La risposta si è interrotta inaspettatamente. Riprova l'ultimo messaggio o controlla lo stato del runtime.",
        },
        reasoning: {
          show: 'Mostra ragionamento',
          hide: 'Nascondi ragionamento',
        },
        tool: {
          showRaw: 'Mostra risultato grezzo',
          hideRaw: 'Nascondi risultato grezzo',
          status: {
            running: 'In corso',
            done: 'Completato',
            error: 'Errore',
          },
        },
      },
      conversations: {
        heading: 'Conversazioni',
        loading: 'Caricamento conversazioni...',
        loadError: 'Impossibile caricare le conversazioni. Aggiorna la lista.',
        untitled: 'Senza titolo',
        includeArchived: 'Mostra archiviate',
        archivedTag: 'Archiviata',
        renameLabel: 'Titolo conversazione',
        empty: {
          heading: 'Avvia una esecuzione',
          body: 'Fai una domanda ad Aura per iniziare. Le tue conversazioni appariranno qui.',
        },
        actions: {
          rename: 'Rinomina',
          archive: 'Archivia',
          unarchive: 'Ripristina',
          delete: 'Elimina definitivamente',
        },
        delete: {
          title: 'Eliminare la conversazione?',
          body: 'Questo elimina definitivamente "{{title}}" e i suoi messaggi. Non è reversibile.',
          confirm: 'Elimina definitivamente',
          cancel: 'Conserva la conversazione',
        },
        search: {
          label: 'Cerca nelle conversazioni',
          placeholder: 'Cerca nelle conversazioni',
          searching: 'Ricerca in corso...',
          empty: {
            heading: 'Nessun risultato',
            body: 'Nessuna conversazione contiene "{{query}}". Prova un altro termine.',
          },
        },
      },
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
        card: {
          freeText: 'La tua risposta',
          freeTextPlaceholder: 'Scrivi la tua risposta',
          answer: 'Rispondi',
          decline: 'Rifiuta',
          cancel: 'Annulla esecuzione',
          declined: "L'agente continuerà, informato del tuo rifiuto.",
          answered: 'Risposto — esecuzione ripresa.',
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
      footer: {
        tokens: 'Token',
        cache: 'Cache',
        cost: 'Costo',
        context: 'Contesto',
        perTurn: 'Questo turno',
        session: 'Sessione',
        none: '—',
        gaugeValue: '{{used}} / {{window}} · {{percent}}%',
        contextLabel: 'Budget di contesto',
        compacted: 'Compattati {{count}} turni più vecchi',
      },
      skeleton: {
        shell: 'Caricamento cockpit...',
        login: 'Caricamento accesso...',
        page: 'Caricamento pagina...',
      },
      health: {
        title: 'Stato runtime',
        loading: 'Controllo runtime...',
        unreachable:
          'Impossibile leggere lo stato runtime. Gli endpoint di health non rispondono: il server potrebbe essere in avvio o non attivo.',
        labels: {
          liveness: 'Vitalità',
          readiness: 'Prontezza',
          postgres: 'Postgres',
          neo4j: 'Neo4j',
          bindAddress: 'Indirizzo bind',
          build: 'Build',
        },
        status: {
          unavailable: 'Non disponibile',
          live: 'Attivo',
          ready: 'Pronto',
          degraded: 'Degradato',
          unknown: 'Sconosciuto',
        },
        lastChecked: 'Ultimo controllo {{time}}',
        relative: {
          never: 'mai',
          justNow: 'ora',
          secondsAgo: '{{count}}s fa',
          minutesAgo: '{{count}}m fa',
        },
      },
      notFound: {
        title: 'Pagina non trovata',
        beforeLink: 'Questa pagina cockpit non esiste.',
        link: 'Torna alla dashboard',
      },
      errorBoundary: {
        title: 'Aura non può mostrare questa vista.',
        action: 'Ricarica per riprovare.',
      },
    },
  },
} as const;

export type AppLanguage = keyof typeof resources;
