// The `onboarding.*` i18n feature bundle (Phase 28 onboarding + provisioning wizard) — split
// out of resources.ts to keep that file under the 600-LOC cap (CLAUDE.md "no god class"), the
// resources.governance.ts / resources.graph.ts precedent. Every leaf string is from the
// 28-UI-SPEC §Copywriting "Onboarding + provisioning wizard" table (the structural keys —
// step labels, field aria, progress, completion — extend it for the full-screen wizard chrome).
// resources.ts spreads onboardingEn/onboardingIt into each language's `translation` object.
// Add every key to BOTH en AND it — a missing key in either language is a defect.

export const onboardingEn = {
  onboarding: {
    title: 'Create identity',
    open: 'Create identity',
    close: 'Close',
    cancel: 'Cancel',
    backendUnavailable:
      "Couldn't start onboarding. The service may be unavailable. Retry, or check the runtime status.",
    authExpired: 'Your session expired. Sign in again to continue.',
    starting: 'Starting…',
    retry: 'Retry',
    back: 'Back',
    progress: 'Step {{current}} of {{total}}',
    cta: {
      continue: 'Continue',
      provision: 'Create identity',
      provisionInFlight: 'Creating identity…',
    },
    steps: {
      credentials: 'Credentials',
      capabilities: 'Capabilities',
      review: 'Review',
      telegram: 'Telegram',
    },
    seed: {
      heading: 'About the operator',
      intro:
        'Aura writes these straight into its memory graph — no interview, no model in the loop. Every field is optional; leave them blank and Aura will learn them from use instead.',
      tooLong: 'That value is too long. Shorten it and try again.',
      name: {
        label: 'Name',
        help: 'How Aura should address you. Stored exactly as typed.',
        placeholder: 'Davide',
        required: 'Add your name — everything else you fill in is stored about it.',
      },
      lang: {
        label: 'Language',
        help: 'The language Aura replies in.',
        unset: 'Not specified',
      },
      location: {
        label: 'Where you are',
        help: 'City or region. Becomes a place Aura can reason about.',
        placeholder: 'Caraglio',
      },
      timezone: {
        label: 'Time zone',
        help: 'Used for schedules and reminders.',
        placeholder: 'Europe/Rome',
      },
      role: {
        label: 'What you do',
        help: 'Your role, in your own words.',
        placeholder: 'founder',
      },
      company: {
        label: 'Organisation',
        help: 'Company, team, or project you work with.',
        placeholder: 'PmSync',
      },
    },
    credentials: {
      heading: 'New operator credentials',
      emailLabel: 'Operator email',
      emailPlaceholder: 'name@example.com',
      passwordLabel: 'Initial password',
      confirmPasswordLabel: 'Confirm initial password',
      passwordHint:
        'The new user sets up two-factor sign-in on first login. The password is never shown again.',
      passwordMismatch: 'The passwords do not match.',
      securityQuestionLabel: 'Security question',
      securityAnswerLabel: 'Security answer',
      securityAnswerHint:
        'Used with Telegram to reset this password. The answer is never shown again.',
    },
    capabilities: {
      label: 'Capabilities for the new identity',
      hint: "You can grant only capabilities you hold. Full-access (`*`) can't be granted.",
      none: 'You hold no grantable capabilities. The new identity will be created with none.',
    },
    telegram: {
      deepLinkCta: 'Open in Telegram',
      qrCaption: 'Or scan to link Telegram',
      qrAlt: 'QR code to link Telegram',
      linked: 'Telegram linked',
      waiting: 'Waiting for the link to be scanned…',
      expired: 'This link expired. Generate a new one to continue.',
      none: 'No Telegram link was generated for this identity.',
    },
    review: {
      heading: 'Review and create',
      emailLabel: 'Operator email',
      capabilitiesLabel: 'Capabilities',
      telegramLabel: 'Telegram',
      telegramRequired: 'Required for password reset',
      noCapabilities: 'None',
    },
    complete: {
      heading: 'Identity created',
      body: 'The new identity can now sign in. Anything you entered above is already in its memory.',
      done: 'Done',
    },
    error: {
      noCapability: "You don't have permission to create an identity.",
      duplicate: 'That email is empty or already in use. Choose another.',
      rolledBack: "Couldn't finish creating the identity, so nothing was saved. Try again.",
    },
    profile: {
      kicker: 'First-run setup',
      title: 'Set up your profile',
      heading: 'Finish setting up Aura',
      body: 'Create the operator profile Aura will use for chat context, saved preferences, and automation handoffs.',
      progressLabel: 'Profile setup progress',
      currentStep: 'Step {{current}} of {{total}}',
      steps: {
        identity: {
          label: 'About you',
          help: 'A handful of typed fields Aura saves straight to memory. All optional.',
        },
        runtime: {
          label: 'Model and token setup',
          help: 'Choose cloud or local model routing, token budgets, and sidecar backends before Aura starts regular work.',
        },
        telegram: {
          label: 'Telegram integration',
          help: 'Connect the Telegram bot Aura uses for the recovery link and chat channel, then scan the pairing QR.',
        },
      },
      runtime: {
        save: 'Save and continue',
        skip: 'Skip runtime setup',
      },
      telegram: {
        checking: 'Checking Telegram configuration…',
        intro:
          'Paste the bot token from @BotFather. Aura validates it live and uses this single bot for the whole instance.',
        tokenLabel: 'Telegram bot token',
        tokenPlaceholder: '123456789:AA…',
        verify: 'Verify token',
        verifying: 'Verifying…',
        valid: 'Valid — bot @{{bot}}',
        invalid: 'That token was rejected by Telegram. Check it and try again.',
        saving: 'Saving…',
        activateNote:
          'Token saved. Restart Aura to activate the bot channel so the pairing scan completes.',
        alreadyConfigured: 'Telegram bot @{{bot}} is already configured.',
        continue: 'Continue',
        errorSave: "Couldn't save the token. Try again.",
        skip: 'Skip Telegram setup',
      },
      skipSetup: 'Skip profile setup',
      saving: 'Saving profile...',
      saveError: "Couldn't save the profile. Try again.",
      completeHeading: 'Profile ready',
      completeBody:
        'What you entered is now in Aura’s memory, and Aura keeps it up to date as you work.',
      skippedBody:
        'Profile setup was skipped. Aura will build your profile from how you work instead.',
    },
  },
} as const;

export const onboardingIt = {
  onboarding: {
    title: 'Crea identità',
    open: 'Crea identità',
    close: 'Chiudi',
    cancel: 'Annulla',
    backendUnavailable:
      'Impossibile avviare la procedura. Il servizio potrebbe non essere disponibile. Riprova, o controlla lo stato del runtime.',
    authExpired: 'La tua sessione è scaduta. Accedi di nuovo per continuare.',
    starting: 'Avvio…',
    retry: 'Riprova',
    back: 'Indietro',
    progress: 'Passaggio {{current}} di {{total}}',
    cta: {
      continue: 'Continua',
      provision: 'Crea identità',
      provisionInFlight: 'Creazione identità…',
    },
    steps: {
      credentials: 'Credenziali',
      capabilities: 'Capacità',
      review: 'Riepilogo',
      telegram: 'Telegram',
    },
    seed: {
      heading: "Chi è l'operatore",
      intro:
        'Aura scrive questi dati direttamente nel suo grafo di memoria — nessuna intervista, nessun modello di mezzo. Ogni campo è facoltativo: se li lasci vuoti, Aura li imparerà dall’uso.',
      tooLong: 'Valore troppo lungo. Accorcialo e riprova.',
      name: {
        label: 'Nome',
        help: 'Come Aura deve chiamarti. Salvato esattamente come lo scrivi.',
        placeholder: 'Davide',
        required: 'Aggiungi il tuo nome — tutto il resto viene salvato riferito a lui.',
      },
      lang: {
        label: 'Lingua',
        help: 'La lingua in cui Aura risponde.',
        unset: 'Non specificata',
      },
      location: {
        label: 'Dove sei',
        help: 'Città o zona. Diventa un luogo su cui Aura può ragionare.',
        placeholder: 'Caraglio',
      },
      timezone: {
        label: 'Fuso orario',
        help: 'Usato per pianificazioni e promemoria.',
        placeholder: 'Europe/Rome',
      },
      role: {
        label: 'Cosa fai',
        help: 'Il tuo ruolo, con parole tue.',
        placeholder: 'founder',
      },
      company: {
        label: 'Organizzazione',
        help: 'Azienda, team o progetto con cui lavori.',
        placeholder: 'PmSync',
      },
    },
    credentials: {
      heading: 'Credenziali del nuovo operatore',
      emailLabel: 'Email operatore',
      emailPlaceholder: 'nome@esempio.com',
      passwordLabel: 'Password iniziale',
      confirmPasswordLabel: 'Conferma password iniziale',
      passwordHint:
        "Il nuovo utente configura l'accesso a due fattori al primo login. La password non verrà più mostrata.",
      passwordMismatch: 'Le password non corrispondono.',
      securityQuestionLabel: 'Domanda di sicurezza',
      securityAnswerLabel: 'Risposta di sicurezza',
      securityAnswerHint:
        'Usata con Telegram per resettare questa password. La risposta non viene piu mostrata.',
    },
    capabilities: {
      label: 'Capacità per la nuova identità',
      hint: "Puoi concedere solo le capacità che possiedi. L'accesso completo (`*`) non è concedibile.",
      none: 'Non possiedi capacità concedibili. La nuova identità verrà creata senza nessuna.',
    },
    telegram: {
      deepLinkCta: 'Apri in Telegram',
      qrCaption: 'Oppure scansiona per collegare Telegram',
      qrAlt: 'Codice QR per collegare Telegram',
      linked: 'Telegram collegato',
      waiting: 'In attesa della scansione del link…',
      expired: 'Questo link è scaduto. Generane uno nuovo per continuare.',
      none: 'Nessun link Telegram è stato generato per questa identità.',
    },
    review: {
      heading: 'Rivedi e crea',
      emailLabel: 'Email operatore',
      capabilitiesLabel: 'Capacità',
      telegramLabel: 'Telegram',
      telegramRequired: 'Richiesto per il reset password',
      noCapabilities: 'Nessuna',
    },
    complete: {
      heading: 'Identità creata',
      body: 'La nuova identità può ora accedere. Quello che hai inserito è già nella sua memoria.',
      done: 'Fatto',
    },
    profile: {
      kicker: 'Primo avvio',
      title: 'Configura il tuo profilo',
      heading: 'Completa la configurazione di Aura',
      body: 'Crea il profilo operatore che Aura usera per contesto chat, preferenze salvate e automazioni.',
      progressLabel: 'Avanzamento configurazione profilo',
      currentStep: 'Passaggio {{current}} di {{total}}',
      steps: {
        identity: {
          label: 'Chi sei',
          help: 'Pochi campi che Aura salva direttamente in memoria. Tutti facoltativi.',
        },
        runtime: {
          label: 'Configurazione modello e token',
          help: 'Scegli modello cloud o locale, budget token e backend sidecar prima del lavoro regolare.',
        },
        telegram: {
          label: 'Integrazione Telegram',
          help: 'Collega il bot Telegram che Aura usa per il link di recupero e il canale chat, poi scansiona il QR di abbinamento.',
        },
      },
      runtime: {
        save: 'Salva e continua',
        skip: 'Salta configurazione runtime',
      },
      telegram: {
        checking: 'Verifica configurazione Telegram…',
        intro:
          'Incolla il token del bot da @BotFather. Aura lo valida in tempo reale e usa questo unico bot per tutta l’istanza.',
        tokenLabel: 'Token bot Telegram',
        tokenPlaceholder: '123456789:AA…',
        verify: 'Verifica token',
        verifying: 'Verifica…',
        valid: 'Valido — bot @{{bot}}',
        invalid: 'Token rifiutato da Telegram. Controllalo e riprova.',
        saving: 'Salvataggio…',
        activateNote:
          'Token salvato. Riavvia Aura per attivare il canale bot e completare la scansione di abbinamento.',
        alreadyConfigured: 'Il bot Telegram @{{bot}} è già configurato.',
        continue: 'Continua',
        errorSave: 'Impossibile salvare il token. Riprova.',
        skip: 'Salta configurazione Telegram',
      },
      skipSetup: 'Salta configurazione profilo',
      saving: 'Salvataggio profilo...',
      saveError: 'Impossibile salvare il profilo. Riprova.',
      completeHeading: 'Profilo pronto',
      completeBody:
        'Quello che hai inserito è ora nella memoria di Aura, che lo tiene aggiornato mentre lavori.',
      skippedBody:
        'Configurazione profilo saltata. Aura costruirà il tuo profilo dal modo in cui lavori.',
    },
    error: {
      noCapability: "Non hai i permessi per creare un'identità.",
      duplicate: "Quell'email è vuota o già in uso. Scegline un'altra.",
      rolledBack:
        "Impossibile completare la creazione dell'identità, quindi nulla è stato salvato. Riprova.",
    },
  },
} as const;
