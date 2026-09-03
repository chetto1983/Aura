export const settingsEn = {
  settings: {
    rail: {
      label: 'Settings sections',
    },
    groups: {
      personal: 'Personal',
      runtime: 'Runtime',
      access: 'Access & channels',
    },
    loading: 'Loading runtime settings...',
    error: "Couldn't load runtime settings. Check the server and try again.",
    restartRequired:
      'Saved changes are in Postgres. Restart Aura to apply them to already-created model clients.',
    restartRequiredFor: 'Saved. Restart Aura to apply: {{keys}}. Everything else is already live.',
    applied: {
      live: 'Applies immediately',
      boot: 'Applied at start-up',
      restart: 'Saved — needs a restart',
    },
    saved: 'Runtime settings saved.',
    saveError:
      "Couldn't save: {{message}}. Your changes are still here — fix the value and try again.",
    secretPlaceholder: 'Enter a new value',
    identity: {
      heading: 'Identities & access',
      body: 'Create operator identities, connect their setup channels, and grant or revoke what each one is allowed to do.',
    },
    standingApprovals: {
      heading: 'Standing approvals',
      body: 'Actions you told Aura to always approve. Until you revoke one here, Aura runs it without stopping to ask.',
      empty: 'No standing approvals. Aura asks before every destructive action.',
      error: 'Could not load your standing approvals.',
      revoke: 'Revoke',
      revokeError: 'Could not revoke that approval. Nothing changed.',
      grantedAt: 'Granted {{date}}',
    },
    telegram: {
      heading: 'Telegram',
      body: 'Add or validate the Telegram bot token, then mint a QR link for this signed-in identity if onboarding was skipped.',
      loading: 'Loading Telegram settings...',
      error: "Couldn't load Telegram settings. Check the server and try again.",
      notConfigured: 'Telegram bot token is not configured.',
      available: 'Bot available: @{{username}}',
      unavailable: 'Telegram bot is not available with that token.',
      requiresRestart:
        'Restart Aura after saving a new token before the Telegram bot can receive scans.',
      saved: 'Telegram token saved. Restart Aura to start the bot with it.',
      qrHeading: 'Link this identity',
      qrBody:
        'Create a one-hour Telegram QR for the current signed-in identity, then scan it from Telegram.',
      qrAlt: 'Telegram setup QR code',
      qrCaption: 'Scan to link Telegram',
      linked: 'Telegram linked.',
      waiting: 'Waiting for scan.',
      actionError: 'Telegram setup failed. Check the token, restart state, and try again.',
      actions: {
        check: 'Check availability',
        save: 'Save Telegram token',
        createQr: 'Create Telegram QR',
        checkLink: 'Check link status',
      },
    },
    modelRouting: {
      heading: 'Model routing',
      body: 'Choose OpenRouter, the local llama.cpp server, or Ollama, then set the model and token budget.',
    },
    provider: {
      label: 'Primary model provider',
      cloud: 'Cloud',
      local: 'Local',
      ollama: 'Ollama',
    },
    models: {
      listLabel: 'Models published by this endpoint',
      count_one: '{{count}} model published here',
      count_other: '{{count}} models published here',
      loading: 'Asking the endpoint what it serves...',
      error: "Couldn't list models: {{message}}. Type the id yourself.",
      noCharge: 'no token charge',
      notPublished: 'not published by this endpoint',
      search: 'Search models...',
      empty: 'No published model matches.',
      useTyped: 'Use "{{model}}" as typed',
      refresh: 'Refresh',
    },
    tokens: {
      heading: 'Token and turn budget',
      body: 'Tune the response cap, context window, compaction trigger and per-turn step limit Aura uses when running a turn.',
    },
    help: {
      maxTokens: 'Per-request cap on the answer sent to the provider (max_tokens).',
      contextWindow:
        'Total tokens the model can hold; the budget ladder and the footer gauge use it.',
      maxOutputTokens:
        'Tokens reserved for the answer when budgeting the prompt — the reservation, not the cap.',
      loopMaxSteps:
        'LLM calls or tool rounds one turn may spend before Aura wraps up (default 25).',
      loopMaxWallclock:
        'Seconds one turn may run, tools included, before Aura wraps up (default 300).',
    },
    backends: {
      heading: 'Sidecar and cloud backends',
      body: 'Swap embeddings, speech, vision, and memory embedding backends between local services and cloud models.',
    },
    fields: {
      primaryModel: 'Primary model',
      primaryBaseUrl: 'Primary base URL',
      primaryProvider: 'Primary provider',
      openRouterKey: 'OpenRouter API key',
      maxTokens: 'Max response tokens',
      contextWindow: 'Context window tokens',
      maxOutputTokens: 'Reserved output tokens',
      compactionTrigger: 'Compact history at % of window',
      loopMaxSteps: 'Max steps per turn',
      loopMaxWallclock: 'Max seconds per turn',
      embedBaseUrl: 'Embedding base URL',
      embedModel: 'Embedding model',
      embedDimensions: 'Embedding dimensions',
      sttCloudModel: 'Speech-to-text cloud model',
      ttsModel: 'Text-to-speech model',
      visionCloud: 'Vision uses cloud',
      telegramBotToken: 'Telegram bot token',
      enabled: 'Enabled',
    },
    status: {
      configured: 'Configured',
      notConfigured: 'Not set',
      active: 'Active',
      inactive: 'Inactive',
    },
    actions: {
      save: 'Save runtime settings',
      continue: 'Continue',
      skip: 'Skip runtime setup',
      reset: 'Reset',
      retry: 'Retry',
    },
  },
} as const;

export const settingsIt = {
  settings: {
    rail: {
      label: 'Sezioni impostazioni',
    },
    groups: {
      personal: 'Personale',
      runtime: 'Runtime',
      access: 'Accesso e canali',
    },
    loading: 'Caricamento impostazioni runtime...',
    error: 'Impossibile caricare le impostazioni runtime. Controlla il server e riprova.',
    restartRequired:
      'Modifiche salvate in Postgres. Riavvia Aura per applicarle ai client modello gia creati.',
    restartRequiredFor: 'Salvato. Riavvia Aura per applicare: {{keys}}. Il resto è già attivo.',
    applied: {
      live: 'Si applica subito',
      boot: 'Applicato all’avvio',
      restart: 'Salvato — richiede riavvio',
    },
    saved: 'Impostazioni runtime salvate.',
    saveError:
      'Salvataggio non riuscito: {{message}}. Le modifiche sono ancora qui — correggi il valore e riprova.',
    secretPlaceholder: 'Inserisci un nuovo valore',
    identity: {
      heading: 'Identità e permessi',
      body: 'Crea identità operatore, collega i loro canali di setup e concedi o revoca cosa ognuna può fare.',
    },
    standingApprovals: {
      heading: 'Approvazioni permanenti',
      body: 'Azioni per cui hai detto ad Aura di approvare sempre. Finché non le revochi qui, Aura le esegue senza fermarsi a chiedere.',
      empty: 'Nessuna approvazione permanente. Aura chiede prima di ogni azione distruttiva.',
      error: 'Non riesco a caricare le tue approvazioni permanenti.',
      revoke: 'Revoca',
      revokeError: 'Non sono riuscito a revocare questa approvazione. Non è cambiato niente.',
      grantedAt: 'Concessa il {{date}}',
    },
    telegram: {
      heading: 'Telegram',
      body: 'Aggiungi o valida il token del bot Telegram, poi crea un QR per questa identita se il collegamento e stato saltato durante onboarding.',
      loading: 'Caricamento impostazioni Telegram...',
      error: 'Impossibile caricare le impostazioni Telegram. Controlla il server e riprova.',
      notConfigured: 'Token bot Telegram non configurato.',
      available: 'Bot disponibile: @{{username}}',
      unavailable: 'Bot Telegram non disponibile con quel token.',
      requiresRestart:
        'Riavvia Aura dopo aver salvato un nuovo token prima che il bot Telegram riceva le scansioni.',
      saved: 'Token Telegram salvato. Riavvia Aura per avviare il bot con questo token.',
      qrHeading: 'Collega questa identita',
      qrBody:
        'Crea un QR Telegram valido un ora per l identita autenticata, poi scansionalo da Telegram.',
      qrAlt: 'Codice QR setup Telegram',
      qrCaption: 'Scansiona per collegare Telegram',
      linked: 'Telegram collegato.',
      waiting: 'In attesa della scansione.',
      actionError: 'Setup Telegram non riuscito. Controlla token e stato del riavvio, poi riprova.',
      actions: {
        check: 'Controlla disponibilita',
        save: 'Salva token Telegram',
        createQr: 'Crea QR Telegram',
        checkLink: 'Controlla stato link',
      },
    },
    modelRouting: {
      heading: 'Instradamento modello',
      body: 'Scegli OpenRouter, il server llama.cpp locale oppure Ollama, poi imposta modello e budget token.',
    },
    provider: {
      label: 'Provider modello primario',
      cloud: 'Cloud',
      local: 'Locale',
      ollama: 'Ollama',
    },
    models: {
      listLabel: 'Modelli pubblicati da questo endpoint',
      count_one: '{{count}} modello pubblicato qui',
      count_other: '{{count}} modelli pubblicati qui',
      loading: 'Sto chiedendo all’endpoint cosa serve...',
      error: 'Impossibile elencare i modelli: {{message}}. Scrivi tu l’id.',
      noCharge: 'nessun costo a token',
      notPublished: 'non pubblicato da questo endpoint',
      search: 'Cerca modelli...',
      empty: 'Nessun modello pubblicato corrisponde.',
      useTyped: 'Usa "{{model}}" così com’è',
      refresh: 'Aggiorna',
    },
    tokens: {
      heading: 'Budget token e turni',
      body: 'Regola limite risposta, finestra contesto, soglia di compattazione e numero massimo di passi per turno.',
    },
    help: {
      maxTokens: 'Tetto per richiesta sulla risposta inviato al provider (max_tokens).',
      contextWindow:
        'Token totali che il modello può tenere; la scala dei budget e l’indicatore nel footer lo usano.',
      maxOutputTokens:
        'Token riservati alla risposta quando si calcola il budget del prompt — la riserva, non il tetto.',
      loopMaxSteps:
        'Chiamate LLM o giri di strumenti che un turno può spendere prima che Aura concluda (default 25).',
      loopMaxWallclock:
        'Secondi che un turno può durare, strumenti inclusi, prima che Aura concluda (default 300).',
    },
    backends: {
      heading: 'Sidecar e backend cloud',
      body: 'Scambia embedding, voce, visione e memoria tra servizi locali e modelli cloud.',
    },
    fields: {
      primaryModel: 'Modello primario',
      primaryBaseUrl: 'URL base primario',
      primaryProvider: 'Provider primario',
      openRouterKey: 'Chiave API OpenRouter',
      maxTokens: 'Token massimi risposta',
      contextWindow: 'Token finestra contesto',
      maxOutputTokens: 'Token output riservati',
      compactionTrigger: 'Comprimi la cronologia al % della finestra',
      loopMaxSteps: 'Passi massimi per turno',
      loopMaxWallclock: 'Secondi massimi per turno',
      embedBaseUrl: 'URL base embedding',
      embedModel: 'Modello embedding',
      embedDimensions: 'Dimensioni embedding',
      sttCloudModel: 'Modello cloud speech-to-text',
      ttsModel: 'Modello text-to-speech',
      visionCloud: 'Visione usa cloud',
      telegramBotToken: 'Token bot Telegram',
      enabled: 'Attivo',
    },
    status: {
      configured: 'Configurato',
      notConfigured: 'Non impostato',
      active: 'Attivo',
      inactive: 'Disattivato',
    },
    actions: {
      save: 'Salva impostazioni runtime',
      continue: 'Continua',
      skip: 'Salta configurazione runtime',
      reset: 'Ripristina',
      retry: 'Riprova',
    },
  },
} as const;

export const profileEn = {
  profile: {
    heading: 'Your profile',
    body: 'What Aura knows about you by declaration rather than by inference. It rides every turn, so keep it current — and clear anything that stops being true.',
    loading: 'Loading your profile...',
    error: "Couldn't load your profile. Check the server and try again.",
    saved: 'Profile saved.',
    saveError:
      "Couldn't save: {{message}}. Your changes are still here — fix the value and try again.",
    fields: {
      name: 'Name',
      role: 'Role',
      company: 'Company',
      location: 'Location',
      timezone: 'Time zone',
      lang: 'Language',
      tonePreference: 'Tone',
      responseLength: 'Response length',
      expertise: 'Expertise',
      stack: 'Stack',
      projects: 'Projects',
      goals: 'Goals',
      interests: 'Interests',
      people: 'People',
      customInstructions: 'Custom instructions',
      vetoes: 'Never do',
    },
    hints: {
      list: 'Comma-separated.',
      customInstructions: 'A sentence or two Aura should follow in every conversation.',
      vetoes:
        'Hard rules, comma-separated. These are prohibitions, not preferences — Aura is told them on every turn.',
    },
    actions: {
      save: 'Save profile',
    },
  },
} as const;

export const profileIt = {
  profile: {
    heading: 'Il tuo profilo',
    body: "Quello che Aura sa di te perche gliel'hai detto, non perche l'ha dedotto. Viaggia in ogni turno: tienilo aggiornato e cancella cio che non e piu vero.",
    loading: 'Caricamento del profilo...',
    error: 'Impossibile caricare il profilo. Controlla il server e riprova.',
    saved: 'Profilo salvato.',
    saveError:
      'Salvataggio non riuscito: {{message}}. Le modifiche sono ancora qui — correggi il valore e riprova.',
    fields: {
      name: 'Nome',
      role: 'Ruolo',
      company: 'Azienda',
      location: 'Dove sei',
      timezone: 'Fuso orario',
      lang: 'Lingua',
      tonePreference: 'Tono',
      responseLength: 'Lunghezza risposte',
      expertise: 'Competenze',
      stack: 'Stack',
      projects: 'Progetti',
      goals: 'Obiettivi',
      interests: 'Interessi',
      people: 'Persone',
      customInstructions: 'Istruzioni personali',
      vetoes: 'Non fare mai',
    },
    hints: {
      list: 'Separati da virgola.',
      customInstructions: 'Una o due frasi che Aura deve seguire in ogni conversazione.',
      vetoes:
        'Regole dure, separate da virgola. Sono divieti, non preferenze — Aura le legge a ogni turno.',
    },
    actions: {
      save: 'Salva profilo',
    },
  },
} as const;
