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
      confirm: 'Looks right — continue',
      edit: 'Edit answer',
      applyEdit: 'Save changes',
      skip: 'Skip this step',
      provision: 'Create identity',
      provisionInFlight: 'Creating identity…',
    },
    steps: {
      credentials: 'Credentials',
      capabilities: 'Capabilities',
      interview: 'Interview',
      review: 'Review',
      telegram: 'Telegram',
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
    interview: {
      answerLabel: 'Your answer',
      answerPlaceholder: 'Type your answer',
      emptyAnswer: 'No answer recorded for this step.',
      draftHeading: 'Profile draft',
      editNameLabel: 'Preferred name',
      editRoleLabel: 'Role or correction',
    },
    telegram: {
      deepLinkCta: 'Open in Telegram',
      qrCaption: 'Or scan to link Telegram',
      qrAlt: 'QR code to link Telegram',
      linked: 'Telegram linked',
      waiting: 'Waiting for the link to be scanned…',
      expired: 'This link expired. Generate a new one to continue.',
      skip: 'Skip Telegram for now',
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
      body: 'The new identity can now sign in. Its Agent.md profile has been saved.',
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
          help: 'Name, role, team, location, timezone, and preferred language.',
        },
        work: {
          label: 'Work stack',
          help: 'Core expertise, tools, systems, and technical environment.',
        },
        projects: {
          label: 'Projects and goals',
          help: 'Active projects, priorities, deadlines, and what success looks like.',
        },
        social: {
          label: 'People and interests',
          help: 'Recurring collaborators, stakeholders, topics, and personal context.',
        },
        style: {
          label: 'Response style',
          help: 'Tone, answer length, language, and voice preferences.',
        },
        draft: {
          label: 'Review profile',
          help: 'Confirm the generated Agent.md profile or edit details before saving.',
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
      prompts: {
        identity:
          'Tell Aura what to call you, your role, team or company, location, timezone, and preferred language.',
        work: 'Share your main areas of expertise and the tools or stack you use most often.',
        projects: 'List what you are working on now and the goals Aura should remember.',
        social:
          'Add recurring collaborators, stakeholders, interests, and topics Aura should keep in context.',
        style:
          'Describe how Aura should respond: tone, answer length, language, and whether voice replies are useful.',
        draft: 'Review the generated Agent.md profile. Confirm it, edit it, or skip profile setup.',
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
      },
      skipSetup: 'Skip profile setup',
      saving: 'Saving profile...',
      saveError: "Couldn't save the profile. Try again.",
      completeHeading: 'Profile ready',
      completeBody: 'Aura can now use your Agent.md profile across the operator cockpit.',
      skippedBody:
        'Profile setup was skipped. Aura will ask again only when you restart first-run setup.',
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
      confirm: 'Va bene — continua',
      edit: 'Modifica risposta',
      applyEdit: 'Salva modifiche',
      skip: 'Salta questo passaggio',
      provision: 'Crea identità',
      provisionInFlight: 'Creazione identità…',
    },
    steps: {
      credentials: 'Credenziali',
      capabilities: 'Capacità',
      interview: 'Intervista',
      review: 'Riepilogo',
      telegram: 'Telegram',
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
    interview: {
      answerLabel: 'La tua risposta',
      answerPlaceholder: 'Scrivi la tua risposta',
      emptyAnswer: 'Nessuna risposta registrata per questo passaggio.',
      draftHeading: 'Bozza del profilo',
      editNameLabel: 'Nome preferito',
      editRoleLabel: 'Ruolo o correzione',
    },
    telegram: {
      deepLinkCta: 'Apri in Telegram',
      qrCaption: 'Oppure scansiona per collegare Telegram',
      qrAlt: 'Codice QR per collegare Telegram',
      linked: 'Telegram collegato',
      waiting: 'In attesa della scansione del link…',
      expired: 'Questo link è scaduto. Generane uno nuovo per continuare.',
      skip: 'Salta Telegram per ora',
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
      body: 'La nuova identità può ora accedere. Il suo profilo Agent.md è stato salvato.',
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
          help: 'Nome, ruolo, team, luogo, fuso orario e lingua preferita.',
        },
        work: {
          label: 'Stack di lavoro',
          help: 'Competenze principali, strumenti, sistemi e ambiente tecnico.',
        },
        projects: {
          label: 'Progetti e obiettivi',
          help: 'Progetti attivi, priorita, scadenze e risultati attesi.',
        },
        social: {
          label: 'Persone e interessi',
          help: 'Collaboratori ricorrenti, stakeholder, temi e contesto personale.',
        },
        style: {
          label: 'Stile risposta',
          help: 'Tono, lunghezza, lingua e preferenze voce.',
        },
        draft: {
          label: 'Revisione profilo',
          help: 'Conferma il profilo Agent.md generato o modifica i dettagli prima del salvataggio.',
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
      prompts: {
        identity:
          'Di ad Aura come chiamarti, il tuo ruolo, team o azienda, luogo, fuso orario e lingua preferita.',
        work: 'Condividi competenze principali e strumenti o stack che usi piu spesso.',
        projects: 'Elenca cosa stai seguendo ora e gli obiettivi che Aura deve ricordare.',
        social:
          'Aggiungi collaboratori ricorrenti, stakeholder, interessi e temi che Aura deve tenere in contesto.',
        style:
          'Descrivi come Aura deve rispondere: tono, lunghezza, lingua e se le risposte vocali sono utili.',
        draft: 'Rivedi il profilo Agent.md generato. Confermalo, modificalo o salta il profilo.',
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
      },
      skipSetup: 'Salta configurazione profilo',
      saving: 'Salvataggio profilo...',
      saveError: 'Impossibile salvare il profilo. Riprova.',
      completeHeading: 'Profilo pronto',
      completeBody: 'Aura puo ora usare il tuo profilo Agent.md nella console operatore.',
      skippedBody:
        'Configurazione profilo saltata. Aura la richiedera di nuovo solo quando riavvii il primo avvio.',
    },
    error: {
      noCapability: "Non hai i permessi per creare un'identità.",
      duplicate: "Quell'email è vuota o già in uso. Scegline un'altra.",
      rolledBack:
        "Impossibile completare la creazione dell'identità, quindi nulla è stato salvato. Riprova.",
    },
  },
} as const;
