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
      passwordHint:
        'The new user sets up two-factor sign-in on first login. The password is never shown again.',
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
      telegramOn: 'A link will be generated',
      telegramOff: 'Not linked',
      noCapabilities: 'None',
      linkTelegram: 'Generate a Telegram link for the new identity',
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
      passwordHint:
        "Il nuovo utente configura l'accesso a due fattori al primo login. La password non verrà più mostrata.",
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
      telegramOn: 'Verrà generato un link',
      telegramOff: 'Non collegato',
      noCapabilities: 'Nessuna',
      linkTelegram: 'Genera un link Telegram per la nuova identità',
    },
    complete: {
      heading: 'Identità creata',
      body: 'La nuova identità può ora accedere. Il suo profilo Agent.md è stato salvato.',
      done: 'Fatto',
    },
    error: {
      noCapability: "Non hai i permessi per creare un'identità.",
      duplicate: "Quell'email è vuota o già in uso. Scegline un'altra.",
      rolledBack:
        "Impossibile completare la creazione dell'identità, quindi nulla è stato salvato. Riprova.",
    },
  },
} as const;
