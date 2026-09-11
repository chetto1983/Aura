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
    progress: 'Step {{current}} of {{total}}',
    cta: {
      continue: 'Continue',
      provision: 'Create identity',
      provisionInFlight: 'Creating identity…',
    },
    steps: {
      credentials: 'Credentials',
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
      telegramLabel: 'Telegram',
      telegramRequired: 'Required for password reset',
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
        route: {
          label: 'Model route',
          help: "Choose what Aura's model runs on: OpenRouter, with the management key Aura mints every other key from, or a local server.",
        },
        telegram: {
          label: 'Telegram integration',
          help: 'Connect the Telegram bot Aura uses for the recovery link and chat channel, then scan the pairing QR.',
        },
      },
      route: {
        save: 'Save and continue',
        skip: 'Skip for now',
        ownKey: 'Your OpenRouter key: {{label}}, no spending limit.',
        servicesKey: 'Services key for speech, embeddings and vision: {{label}}.',
        errors: 'OpenRouter refused part of the setup:',
        stillRequired:
          'Aura needs the OpenRouter management key, or a local route, before anyone can chat.',
        restarting: 'Restarting Aura…',
        restartingBody:
          'Speech, embeddings and vision switch to the new services key. It takes about a minute.',
        restartFailed:
          "Aura didn't restart. Your keys already work for chat; restart Aura from Settings so speech, embeddings and vision use the services key.",
        continue: 'Continue',
      },
      telegram: {
        checking: 'Checking Telegram configuration…',
        intro:
          'Paste the bot token from @BotFather. Aura validates it live and uses this single bot for the whole instance.',
        tokenLabel: 'Telegram bot token',
        tokenPlaceholder: '123456789:AA…',
        verify: 'Verify token',
        verifying: 'Verifying…',
        invalid: 'That token was rejected by Telegram. Check it and try again.',
        saving: 'Saving…',
        channelActive: 'Telegram channel active — @{{bot}}',
        channelInactive: "Token saved for @{{bot}}, but the Telegram channel didn't start.",
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
    progress: 'Passaggio {{current}} di {{total}}',
    cta: {
      continue: 'Continua',
      provision: 'Crea identità',
      provisionInFlight: 'Creazione identità…',
    },
    steps: {
      credentials: 'Credenziali',
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
      telegramLabel: 'Telegram',
      telegramRequired: 'Richiesto per il reset password',
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
        route: {
          label: 'Percorso del modello',
          help: 'Scegli su cosa gira il modello di Aura: OpenRouter, con la chiave di gestione da cui Aura crea tutte le altre, oppure un server locale.',
        },
        telegram: {
          label: 'Integrazione Telegram',
          help: 'Collega il bot Telegram che Aura usa per il link di recupero e il canale chat, poi scansiona il QR di abbinamento.',
        },
      },
      route: {
        save: 'Salva e continua',
        skip: 'Salta per ora',
        ownKey: 'La tua chiave OpenRouter: {{label}}, senza limite di spesa.',
        servicesKey: 'Chiave dei servizi per voce, embedding e visione: {{label}}.',
        errors: 'OpenRouter ha rifiutato una parte della configurazione:',
        stillRequired:
          'Aura ha bisogno della chiave di gestione OpenRouter, o di un percorso locale, prima che chiunque possa chattare.',
        restarting: 'Riavvio di Aura…',
        restartingBody:
          'Voce, embedding e visione passano alla nuova chiave dei servizi. Serve circa un minuto.',
        restartFailed:
          'Aura non si è riavviata. Le chiavi funzionano già per la chat; riavvia Aura dalle Impostazioni perché voce, embedding e visione usino la chiave dei servizi.',
        continue: 'Continua',
      },
      telegram: {
        checking: 'Verifica configurazione Telegram…',
        intro:
          'Incolla il token del bot da @BotFather. Aura lo valida in tempo reale e usa questo unico bot per tutta l’istanza.',
        tokenLabel: 'Token bot Telegram',
        tokenPlaceholder: '123456789:AA…',
        verify: 'Verifica token',
        verifying: 'Verifica…',
        invalid: 'Token rifiutato da Telegram. Controllalo e riprova.',
        saving: 'Salvataggio…',
        channelActive: 'Canale Telegram attivo — @{{bot}}',
        channelInactive: 'Token salvato per @{{bot}}, ma il canale Telegram non si è avviato.',
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
