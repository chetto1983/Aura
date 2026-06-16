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
        sections: {
          investigations: 'Investigations',
          searxngSocket: 'SearXNG socket',
          neo4jMcp: 'Neo4j MCP',
        },
        modes: {
          chat: 'Chat',
          tree: 'Tree',
          graph: 'Graph',
          displays: 'Displays',
          settings: 'Settings',
        },
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
        sections: {
          investigations: 'Indagini',
          searxngSocket: 'Socket SearXNG',
          neo4jMcp: 'MCP Neo4j',
        },
        modes: {
          chat: 'Chat',
          tree: 'Albero',
          graph: 'Grafo',
          displays: 'Display',
          settings: 'Impostazioni',
        },
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
