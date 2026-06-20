// The `governance.*` i18n feature bundle (Phase 28 read-only governance boards — MCP /
// Skills / Scheduler) — split out of resources.ts to keep that file under the 600-LOC cap
// (CLAUDE.md "no god class"), the resources.graph.ts / resources.display.ts precedent.
// Every key is from the 28-UI-SPEC §Copywriting Contract. resources.ts spreads
// governanceEn/governanceIt into each language's `translation` object. Add every key to
// BOTH en AND it — a missing key in either language is a defect.

export const governanceEn = {
  governance: {
    title: 'Governance',
    readOnlyBanner: 'Read-only — viewing only. Changes arrive in a later phase.',
    loading: 'Loading…',
    retry: 'Retry',
    closeAria: 'Close',
    detailEmpty: 'Select a row to see details',
    error:
      "Couldn't load this board. The service may be unavailable. Retry, or check the runtime status.",
    authExpired: 'Your session expired. Sign in again to continue.',
    pagination: {
      more: 'Show more',
      status: 'Showing {{shown}} of {{total}}',
    },
    tabs: {
      mcp: 'MCP servers',
      skills: 'Skills',
      scheduler: 'Scheduler',
    },
    mcp: {
      emptyHeading: 'No MCP servers configured',
      emptyBody:
        'No MCP servers are configured yet. Add one from the CLI (`aura mcp`); it will appear here.',
      status: {
        checking: 'Checking…',
        healthy: 'Healthy · {{count}} tools',
        timedOut: "Timed out — server didn't respond",
        error: 'Error — {{state}}',
      },
      redacted: 'redacted',
      detail: {
        trust: 'Trust',
        runtime: 'Runtime',
        startup: 'Startup',
        authStatus: 'Auth status',
        envKeys: 'Environment keys',
        toolCount: 'Tools',
        probeDetail: 'Probe result',
        lastError: 'Last error',
        noEnvKeys: 'No environment keys.',
      },
    },
    skills: {
      emptyHeading: 'No skills yet',
      emptyBody:
        "No skills in this stage yet. Skills appear here as they're installed and used.",
      stages: {
        active: 'Active',
        pending: 'Pending',
        archived: 'Archived',
        audit: 'Audit',
      },
      pendingNote: 'Pending — inactive and cannot be run.',
      auditEmpty: 'No audit entries yet.',
      field: {
        type: 'Type',
        language: 'Language',
        contentHash: 'Content hash',
        description: 'Description',
        none: '—',
      },
      audit: {
        action: 'Action',
        skill: 'Skill',
        actor: 'Actor',
        when: 'When',
        contentHash: 'Content hash',
      },
    },
    scheduler: {
      emptyHeading: 'No scheduled tasks',
      emptyBody:
        'Nothing is scheduled yet. Scheduled jobs and their run history will appear here.',
      field: {
        kind: 'Kind',
        schedule: 'Schedule',
        nextRun: 'Next run',
        status: 'Status',
        none: '—',
      },
      runs: {
        heading: 'Run history',
        empty: 'No runs recorded for this task yet.',
        status: 'Status',
        started: 'Started',
        heartbeat: 'Heartbeat',
        summary: 'Summary',
      },
    },
  },
} as const;

export const governanceIt = {
  governance: {
    title: 'Governance',
    readOnlyBanner:
      'Sola lettura — solo visualizzazione. Le modifiche arriveranno in una fase successiva.',
    loading: 'Caricamento…',
    retry: 'Riprova',
    closeAria: 'Chiudi',
    detailEmpty: 'Seleziona una riga per i dettagli',
    error:
      'Impossibile caricare questa scheda. Il servizio potrebbe non essere disponibile. Riprova, o controlla lo stato del runtime.',
    authExpired: 'La tua sessione è scaduta. Accedi di nuovo per continuare.',
    pagination: {
      more: 'Mostra altro',
      status: 'Mostrate {{shown}} di {{total}}',
    },
    tabs: {
      mcp: 'Server MCP',
      skills: 'Competenze',
      scheduler: 'Pianificazione',
    },
    mcp: {
      emptyHeading: 'Nessun server MCP configurato',
      emptyBody:
        'Nessun server MCP è ancora configurato. Aggiungine uno dalla CLI (`aura mcp`); apparirà qui.',
      status: {
        checking: 'Verifica…',
        healthy: 'Integro · {{count}} strumenti',
        timedOut: 'Tempo scaduto — il server non ha risposto',
        error: 'Errore — {{state}}',
      },
      redacted: 'nascosto',
      detail: {
        trust: 'Affidabilità',
        runtime: 'Runtime',
        startup: 'Avvio',
        authStatus: 'Stato autenticazione',
        envKeys: 'Chiavi ambiente',
        toolCount: 'Strumenti',
        probeDetail: 'Esito verifica',
        lastError: 'Ultimo errore',
        noEnvKeys: 'Nessuna chiave ambiente.',
      },
    },
    skills: {
      emptyHeading: 'Ancora nessuna competenza',
      emptyBody:
        'Nessuna competenza in questa fase. Le competenze appaiono qui man mano che vengono installate e usate.',
      stages: {
        active: 'Attive',
        pending: 'In attesa',
        archived: 'Archiviate',
        audit: 'Audit',
      },
      pendingNote: 'In attesa — inattiva e non eseguibile.',
      auditEmpty: 'Ancora nessuna voce di audit.',
      field: {
        type: 'Tipo',
        language: 'Linguaggio',
        contentHash: 'Hash contenuto',
        description: 'Descrizione',
        none: '—',
      },
      audit: {
        action: 'Azione',
        skill: 'Competenza',
        actor: 'Attore',
        when: 'Quando',
        contentHash: 'Hash contenuto',
      },
    },
    scheduler: {
      emptyHeading: 'Nessuna attività pianificata',
      emptyBody:
        'Niente è ancora pianificato. I job pianificati e la loro cronologia appariranno qui.',
      field: {
        kind: 'Tipo',
        schedule: 'Pianificazione',
        nextRun: 'Prossima esecuzione',
        status: 'Stato',
        none: '—',
      },
      runs: {
        heading: 'Cronologia esecuzioni',
        empty: 'Nessuna esecuzione registrata per questa attività.',
        status: 'Stato',
        started: 'Avviata',
        heartbeat: 'Heartbeat',
        summary: 'Riepilogo',
      },
    },
  },
} as const;
