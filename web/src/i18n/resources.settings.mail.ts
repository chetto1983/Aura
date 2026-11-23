// The «Posta e calendario» Settings pane: the identity's account list, and the connect screen (an
// address and a password, checked before they are saved), and the repair on an IMAP row.
// `verify.<reason>` and `error.<code>` are reached through pimApi's pimErrorKey and mailConnectLogic's
// verifyMessageKey, so every reason the PIM can answer has a sentence here.

export const mailEn = {
  heading: 'Mail and calendar',
  loading: 'Loading accounts…',
  offline: 'Mail and calendar accounts are unavailable or not configured on this deployment.',
  empty: 'No accounts yet. Add one below.',
  accountsHeading: 'Accounts',
  removeError: "Couldn't remove the account. Try again.",
  addressLabel: 'Email address',
  passwordLabel: 'Password',
  connect: 'Connect',
  connecting: 'Checking the sign-in…',
  discovering: 'Looking up the mail servers for this address…',
  connected: 'Connected',
  signIn: {
    google: 'Sign in with Google',
    microsoft: 'Sign in with Microsoft',
  },
  providerAppMissing:
    'The administrator has not set up {{provider}} sign-in yet. You can connect with an app password instead.',
  providerAppMissingNoFallback:
    '{{provider}} only accepts its own sign-in, and the administrator has not set it up yet. Ask the administrator to configure it.',
  useAppPassword: 'Use an app password',
  appPasswordNotice: 'This provider wants an app password here, not the one you sign in with.',
  appPasswordHelp: 'How to create one',
  mxNotice: 'I will connect to {{host}}',
  serverSettings: 'Server settings',
  otherAccountType: 'Other account type',
  conflict: {
    sameAddress: 'This address is already connected',
    leftover: 'An earlier sign-in with this address did not finish. Remove it to connect again.',
  },
  changePassword: 'Change password',
  remove: 'Remove',
  repair: {
    tryAgain: 'Try again',
    loginWorks:
      'Sign-in works. If reading does not resume at the next check, the password is not the problem.',
    newPassword: 'New password',
    serverHint: 'A blank field stays as it is.',
    usernameFixed: 'The username cannot be changed: for another mailbox, add a new account.',
    save: 'Save',
    cancel: 'Cancel',
  },
  verify: {
    appPassword:
      'The password was refused. This provider wants an app password, not the one you sign in with.',
    auth_failed: 'The password was refused. Check it and try again.',
    authFailedRepair:
      'The password was refused: it is wrong, or the provider wants an app password instead of the one you sign in with.',
    unreachable: 'The mail server did not answer. Check the server name and port.',
    tls_failed:
      'The mail server could not prove its identity. Use the server name your provider gives, not one on your own domain.',
    mailbox_refused: 'The sign-in worked, but the server would not open the inbox.',
    smtp_auth_failed: 'The sending server refused the password. Check the SMTP settings.',
    smtp_unreachable: 'The sending server did not answer. Check the SMTP server name and port.',
    smtpWarning: {
      smtp_unreachable:
        'Reading works, but the sending server did not answer: replies cannot be sent yet.',
      tls_failed:
        'Reading works, but the sending server could not prove its identity: replies cannot be sent yet.',
    },
  },
  error: {
    timeout: 'The check took too long. Try again in a moment.',
    generic: 'Could not connect the account. Try again.',
    pimDown: 'The mail service is not answering. Try again in a moment.',
    notReady: 'The mail service is not ready yet. Try again in a moment.',
    tooMany: 'Each try signs in at the provider: wait half a minute and try again.',
    invalidAddress: 'Enter a complete email address.',
    passwordRequired: 'A change of server needs the password again.',
    changed: 'The account changed meanwhile. Reload and try again.',
    invalidHost: 'The server name is not valid: check it under Server settings.',
    invalidPort: 'The port is not valid: use a number between 1 and 65535.',
  },
} as const;

export const mailIt = {
  heading: 'Posta e calendario',
  loading: 'Caricamento account…',
  offline:
    'Gli account di posta e calendario non sono disponibili o non sono configurati su questo deployment.',
  empty: 'Ancora nessun account. Aggiungine uno qui sotto.',
  accountsHeading: 'Account',
  removeError: 'Impossibile rimuovere l’account. Riprova.',
  addressLabel: 'Indirizzo email',
  passwordLabel: 'Password',
  connect: 'Collega',
  connecting: "Verifico l'accesso…",
  discovering: 'Cerco i server di posta di questo indirizzo…',
  connected: 'Collegato',
  signIn: {
    google: 'Accedi con Google',
    microsoft: 'Accedi con Microsoft',
  },
  providerAppMissing:
    "L'amministratore non ha ancora configurato l'accesso con {{provider}}. Puoi collegarti con una password per app.",
  providerAppMissingNoFallback:
    "{{provider}} accetta solo il proprio accesso, e l'amministratore non l'ha ancora configurato. Chiedi all'amministratore di configurarlo.",
  useAppPassword: 'Usa una password per app',
  appPasswordNotice: 'Questo provider vuole qui una password per app, non quella con cui accedi.',
  appPasswordHelp: 'Come crearla',
  mxNotice: 'Mi collegherò a {{host}}',
  serverSettings: 'Impostazioni server',
  otherAccountType: 'Altro tipo di account',
  conflict: {
    sameAddress: 'Questo indirizzo è già collegato',
    leftover:
      'Un accesso precedente con questo indirizzo non è stato completato. Rimuovilo per collegarlo di nuovo.',
  },
  changePassword: 'Cambia password',
  remove: 'Rimuovi',
  repair: {
    tryAgain: 'Prova di nuovo',
    loginWorks:
      "L'accesso funziona. Se la lettura non riprende al prossimo controllo, la password non è il problema.",
    newPassword: 'Nuova password',
    serverHint: 'Un campo lasciato vuoto resta com’è.',
    usernameFixed:
      "Il nome utente non si può cambiare: per un'altra casella aggiungi un nuovo account.",
    save: 'Salva',
    cancel: 'Annulla',
  },
  verify: {
    appPassword:
      'La password è stata rifiutata. Questo provider vuole una password per app, non quella con cui accedi.',
    auth_failed: 'La password è stata rifiutata. Controllala e riprova.',
    authFailedRepair:
      'La password è stata rifiutata: è sbagliata, oppure il provider vuole una password per app invece di quella con cui accedi.',
    unreachable: 'Il server di posta non ha risposto. Controlla il nome del server e la porta.',
    tls_failed:
      'Il server di posta non ha dimostrato la sua identità. Usa il nome del server che ti dà il provider, non uno sul tuo dominio.',
    mailbox_refused: "L'accesso è riuscito, ma il server non ha aperto la posta in arrivo.",
    smtp_auth_failed: "Il server d'invio ha rifiutato la password. Controlla le impostazioni SMTP.",
    smtp_unreachable:
      "Il server d'invio non ha risposto. Controlla il nome del server SMTP e la porta.",
    smtpWarning: {
      smtp_unreachable:
        "La lettura funziona, ma il server d'invio non ha risposto: per ora non si possono inviare risposte.",
      tls_failed:
        "La lettura funziona, ma il server d'invio non ha dimostrato la sua identità: per ora non si possono inviare risposte.",
    },
  },
  error: {
    timeout: 'Il controllo ha richiesto troppo tempo. Riprova tra poco.',
    generic: "Non riesco a collegare l'account. Riprova.",
    pimDown: 'Il servizio di posta non risponde. Riprova tra poco.',
    notReady: 'Il servizio di posta non è ancora pronto. Riprova tra poco.',
    tooMany: 'Ogni prova è un accesso al provider: aspetta mezzo minuto e riprova.',
    invalidAddress: 'Scrivi un indirizzo email completo.',
    passwordRequired: 'Un cambio di server richiede di nuovo la password.',
    changed: "L'account è cambiato nel frattempo. Ricarica e riprova.",
    invalidHost: 'Il nome del server non è valido: controllalo in Impostazioni server.',
    invalidPort: 'La porta non è valida: usa un numero tra 1 e 65535.',
  },
} as const;
