export const remoteAccessEn = {
  remoteAccess: {
    heading: 'Remote access',
    body: 'Open your Aura away from home, from an iPad or a phone, through Cloudflare.',
    loading: 'Loading remote access…',
    loadError: 'Could not load remote access. Check the server and try again.',
    retry: 'Retry',
    steps: {
      label: 'Remote access setup progress',
      cloudflare: 'Cloudflare',
      domain: 'Domain',
      connection: 'Connection',
      phone: 'Try it from your phone',
    },
    nav: { back: 'Back', next: 'Next' },
    failure: {
      stale: 'The configuration changed in the meantime. Aura reloaded it: check it and try again.',
      generic: 'The request failed and the server gave no reason. Try again.',
    },
    cloudflare: {
      heading: 'Connect your Cloudflare account',
      intro:
        'Aura uses an API token to create the tunnel, the address and the Access protection in your account. The token is never shown again.',
      createToken: 'Create the token on Cloudflare',
      tokenHint:
        'The link opens the form with the permissions already chosen. Add Cloudflare Tunnel Edit yourself: it is the one Cloudflare cannot pre-fill.',
      permissions: 'Permissions the token needs',
      tokenLabel: 'Cloudflare API token',
      verify: 'Verify token',
      account: 'Cloudflare account',
      accountPlaceholder: 'Choose the account',
      accountLocked: 'The account changes with a token that sees another one.',
      needToken: 'Paste the token to continue',
      needNewToken: 'Paste a new token to continue',
      needVerify: 'Verify the token to continue',
      needAccount: 'Choose the account to continue',
      abandoned: {
        heading: 'To delete by hand on Cloudflare',
        body: 'These resources are still on Cloudflare and Aura no longer manages them. Delete them from the Cloudflare dashboard, then press Done.',
        done: 'Done',
        kinds: {
          tunnel: 'Tunnel',
          dns: 'DNS record',
          access_app: 'Access application',
          access_policy: 'Access policy',
          posture: 'Device check',
        },
      },
    },
    domain: {
      heading: 'Choose the domain',
      intro:
        'The domain must be yours and in this same Cloudflare account. Aura does not buy it: you register it with a registrar.',
      domain: 'Domain',
      label: 'Address label',
      preview: 'You will open Aura at',
      needDomain: 'Write the domain to continue',
      needLabel: 'Write the label to continue',
      waitingNext: 'Waiting for the nameservers: the next step opens by itself',
      off: 'Remote access is off. Press Next to turn it on at this address.',
      deleting:
        'Aura is still deleting the previous address. You can save now: when it finishes, press Next once more to turn access back on.',
      changeWarning:
        'The current address stops working, and you will repeat the test from your phone.',
      nameservers: {
        heading: 'Change the nameservers at your registrar',
        body: 'Replace the domain’s nameservers with these two. Aura carries on by itself once Cloudflare activates the zone.',
        unread:
          'Aura cannot read the nameservers right now. You will find them in the Cloudflare dashboard, on the domain’s Overview page.',
        waiting: 'Waiting for Cloudflare to activate the zone…',
      },
    },
    connection: {
      heading: 'Connection to Cloudflare',
      running:
        'Aura is checking the connection to Cloudflare and creating whatever is missing: the tunnel, the address and the Access protection.',
      ready: 'The connection is ready.',
      deleting:
        'Aura is deleting what it created in your Cloudflare account. When it finishes, Domain opens again so you can save the address.',
      needWait: 'Wait for Aura to finish: the next step opens by itself',
      needWaitDeleting: 'Wait for Aura to finish deleting to continue',
      needEarlier: 'This step is not ready yet: go back and finish the ones before it',
      needFix: 'Fix the problem above to continue',
      rotation:
        'If you rotated the tunnel token in the Cloudflare dashboard, Aura has to read it again.',
      rotationGuide: 'How to rotate the tunnel token',
      rotationRefresh: 'Refresh Dashboard-rotated token',
    },
    phone: {
      heading: 'Try it from your phone',
      test: 'Open this address from your phone on mobile data, away from the home network. Cloudflare asks for a PIN by email, then you sign in to Aura. Confirm from there, in Settings.',
      open: 'Open the public address',
      reachable: 'Reachable from outside',
      unreachable: 'Not reachable',
      lastChecked: 'Last checked: {{time}}',
      externalOnly: 'The confirmation is only available from that address.',
      accept: 'Confirm external access',
      notReady: 'The connection is not ready yet. Wait a few seconds and press Confirm again.',
      direct:
        'At home you can always open Aura by its IP address: Cloudflare asks for no PIN there, and your Aura sign-in still applies.',
      disable: 'Disable',
      delete: 'Delete…',
    },
    delete: {
      title: 'Delete remote access?',
      body: 'Aura removes what it created on Cloudflare: the tunnel, the address and the Access protection. Your domain and your token stay. Type the exact address to continue.',
      label: 'Type {{hostname}} to confirm',
      confirm: 'Delete permanently',
      cancel: 'Cancel',
    },
    reasons: {
      token_rejected: {
        sentence:
          'Cloudflare refused the token: it has expired, was revoked or lacks a permission.',
        action: 'Paste a new token',
      },
      no_accounts: {
        sentence: 'The token works but sees no account: it lacks Account Settings Read.',
        action: 'Paste a token with that permission',
      },
      account_not_visible: {
        sentence: 'This token cannot see the chosen account: verify it and choose one it can see.',
        action: 'Change the token or the account',
      },
      domain_invalid: {
        sentence:
          'The domain or the label is not valid: use a domain like example.com and a label of letters, digits and hyphens.',
        action: 'Fix the domain',
      },
      domain_elsewhere: {
        sentence:
          'This domain is active in another Cloudflare account, and a tunnel only serves domains of its own account. Aura created nothing.',
        action: 'Use another domain',
      },
      hostname_taken: {
        sentence:
          'Cloudflare already uses this address for another Access application or a DNS record Aura did not create.',
        action: 'Choose another label',
      },
      not_owned: {
        sentence:
          'Cloudflare holds a resource Aura did not create, and Aura leaves what is not its own alone. Remove it in the dashboard or change the domain, then retry.',
        action: 'Retry',
      },
      no_administrator: {
        sentence:
          'No active administrator could get through Cloudflare: Aura stops so you are not locked out. Check the Aura users, then retry.',
        action: 'Retry',
      },
      cloudflare_unreachable: {
        sentence: 'Cloudflare is not answering right now. Try again in a moment.',
        action: 'Retry now',
      },
      tunnel_down: {
        sentence:
          'The tunnel is not connected to Cloudflare. When it comes back this step turns green by itself, with no new test.',
        action: 'Check now',
      },
      old_address_present: {
        sentence:
          'Cloudflare still shows the old address: Aura waits for confirmation that it is gone and keeps trying.',
        action: 'Retry now',
      },
      unknown: {
        sentence: 'Something failed and Aura cannot say what: the detail is in the server logs.',
        action: 'Retry',
      },
    },
  },
} as const;

export const remoteAccessIt = {
  remoteAccess: {
    heading: 'Accesso remoto',
    body: 'Apri la tua Aura da fuori casa, da iPad e telefono, tramite Cloudflare.',
    loading: 'Caricamento accesso remoto…',
    loadError: 'Impossibile caricare l’accesso remoto. Controlla il server e riprova.',
    retry: 'Riprova',
    steps: {
      label: 'Avanzamento configurazione accesso remoto',
      cloudflare: 'Cloudflare',
      domain: 'Dominio',
      connection: 'Collegamento',
      phone: 'Prova dal telefono',
    },
    nav: { back: 'Indietro', next: 'Avanti' },
    failure: {
      stale:
        'La configurazione è cambiata nel frattempo. Aura l’ha ricaricata: controlla e riprova.',
      generic: 'La richiesta non è andata a buon fine e il server non ha detto perché. Riprova.',
    },
    cloudflare: {
      heading: 'Collega il tuo account Cloudflare',
      intro:
        'Aura usa un token API per creare il tunnel, l’indirizzo e la protezione Access nel tuo account. Il token non viene mai mostrato di nuovo.',
      createToken: 'Crea il token su Cloudflare',
      tokenHint:
        'Il link apre il modulo con i permessi già scelti. Aggiungi tu Cloudflare Tunnel Edit: è l’unico che Cloudflare non sa precompilare.',
      permissions: 'Permessi che servono al token',
      tokenLabel: 'Token API Cloudflare',
      verify: 'Verifica token',
      account: 'Account Cloudflare',
      accountPlaceholder: 'Scegli l’account',
      accountLocked: 'L’account cambia con un token che ne vede un altro.',
      needToken: 'Incolla il token per continuare',
      needNewToken: 'Incolla un token nuovo per continuare',
      needVerify: 'Verifica il token per continuare',
      needAccount: 'Scegli l’account per continuare',
      abandoned: {
        heading: 'Da eliminare a mano su Cloudflare',
        body: 'Queste risorse sono rimaste su Cloudflare e Aura non le gestisce più. Eliminale dal dashboard Cloudflare, poi premi Fatto.',
        done: 'Fatto',
        kinds: {
          tunnel: 'Tunnel',
          dns: 'Record DNS',
          access_app: 'Applicazione Access',
          access_policy: 'Regola Access',
          posture: 'Controllo del dispositivo',
        },
      },
    },
    domain: {
      heading: 'Scegli il dominio',
      intro:
        'Il dominio deve essere tuo e in questo stesso account Cloudflare. Aura non lo compra: lo registri tu presso un registrar.',
      domain: 'Dominio',
      label: 'Etichetta dell’indirizzo',
      preview: 'Aprirai Aura da',
      needDomain: 'Scrivi il dominio per continuare',
      needLabel: 'Scrivi l’etichetta per continuare',
      waitingNext: 'In attesa dei nameserver: il passo successivo si apre da solo',
      off: 'L’accesso remoto è spento. Premi Avanti per accenderlo su questo indirizzo.',
      deleting:
        'Aura sta ancora eliminando l’indirizzo precedente. Puoi salvare anche ora: quando ha finito, premi Avanti ancora una volta per riaccendere l’accesso.',
      changeWarning:
        'L’indirizzo attuale smette di funzionare, e dovrai rifare la prova dal telefono.',
      nameservers: {
        heading: 'Cambia i nameserver presso il registrar',
        body: 'Sostituisci i nameserver del dominio con questi due. Aura continua da sola quando Cloudflare attiva la zona.',
        unread:
          'Aura non riesce a leggere i nameserver in questo momento. Li trovi nel dashboard Cloudflare, nella pagina Overview del dominio.',
        waiting: 'In attesa che Cloudflare attivi la zona…',
      },
    },
    connection: {
      heading: 'Collegamento con Cloudflare',
      running:
        'Aura sta controllando il collegamento con Cloudflare e crea quello che manca: tunnel, indirizzo e protezione Access.',
      ready: 'Il collegamento è pronto.',
      deleting:
        'Aura sta eliminando quello che ha creato nel tuo account Cloudflare. Quando ha finito si riapre Dominio, e puoi salvare l’indirizzo.',
      needWait: 'Aspetta che Aura finisca: il passo successivo si apre da solo',
      needWaitDeleting: 'Aspetta che Aura finisca di eliminare per continuare',
      needEarlier: 'Questo passo non è ancora pronto: torna indietro e completa quelli prima',
      needFix: 'Risolvi il problema qui sopra per continuare',
      rotation:
        'Se hai ruotato il token del tunnel nel dashboard Cloudflare, Aura deve rileggerlo.',
      rotationGuide: 'Come ruotare il token del tunnel',
      rotationRefresh: 'Rileggi il token ruotato',
    },
    phone: {
      heading: 'Prova dal telefono',
      test: 'Apri questo indirizzo dal telefono, con i dati mobili e fuori dalla rete di casa. Cloudflare ti chiede un PIN via email, poi entri in Aura. Conferma da lì, in Impostazioni.',
      open: 'Apri l’indirizzo pubblico',
      reachable: 'Raggiungibile da fuori casa',
      unreachable: 'Non raggiungibile',
      lastChecked: 'Ultimo controllo: {{time}}',
      externalOnly: 'La conferma è disponibile solo da quell’indirizzo.',
      accept: 'Conferma l’accesso da fuori',
      notReady:
        'Il collegamento non è ancora pronto. Aspetta qualche secondo e premi di nuovo Conferma.',
      direct:
        'A casa puoi sempre aprire Aura dal suo indirizzo IP: lì Cloudflare non chiede il PIN, e resta il tuo accesso ad Aura.',
      disable: 'Disattiva',
      delete: 'Elimina…',
    },
    delete: {
      title: 'Eliminare l’accesso remoto?',
      body: 'Aura rimuove quello che ha creato su Cloudflare: il tunnel, l’indirizzo e la protezione Access. Il dominio e il token restano. Scrivi l’indirizzo esatto per continuare.',
      label: 'Scrivi {{hostname}} per confermare',
      confirm: 'Elimina definitivamente',
      cancel: 'Annulla',
    },
    reasons: {
      token_rejected: {
        sentence:
          'Cloudflare ha rifiutato il token: è scaduto, è stato revocato o gli manca un permesso.',
        action: 'Incolla un token nuovo',
      },
      no_accounts: {
        sentence: 'Il token funziona ma non vede nessun account: gli manca Account Settings Read.',
        action: 'Incolla un token con quel permesso',
      },
      account_not_visible: {
        sentence: 'Questo token non vede l’account scelto: verificalo e scegline uno che vede.',
        action: 'Cambia token o account',
      },
      domain_invalid: {
        sentence:
          'Il dominio o l’etichetta non vanno bene: serve un dominio come esempio.it e un’etichetta di lettere, numeri e trattini.',
        action: 'Correggi il dominio',
      },
      domain_elsewhere: {
        sentence:
          'Questo dominio è attivo in un altro account Cloudflare, e un tunnel serve solo i domini del suo stesso account. Aura non ha creato nulla.',
        action: 'Usa un altro dominio',
      },
      hostname_taken: {
        sentence:
          'Questo indirizzo è già occupato su Cloudflare da un’altra applicazione Access o da un record DNS che Aura non ha creato.',
        action: 'Scegli un’altra etichetta',
      },
      not_owned: {
        sentence:
          'Su Cloudflare c’è una risorsa che Aura non ha creato, e Aura non tocca ciò che non è suo. Eliminala dal dashboard o cambia dominio, poi riprova.',
        action: 'Riprova',
      },
      no_administrator: {
        sentence:
          'Nessun amministratore attivo potrebbe passare da Cloudflare: Aura si ferma per non chiuderti fuori. Controlla gli utenti Aura, poi riprova.',
        action: 'Riprova',
      },
      cloudflare_unreachable: {
        sentence: 'Cloudflare non risponde in questo momento. Riprova tra poco.',
        action: 'Riprova ora',
      },
      tunnel_down: {
        sentence:
          'Il tunnel non è collegato a Cloudflare. Quando torna su, questo passo ridiventa verde da solo, senza rifare la prova.',
        action: 'Controlla ora',
      },
      old_address_present: {
        sentence:
          'Il vecchio indirizzo risulta ancora su Cloudflare: Aura aspetta la conferma che sia sparito e continua a provare.',
        action: 'Riprova ora',
      },
      unknown: {
        sentence:
          'Qualcosa non ha funzionato e Aura non sa dire cosa: il dettaglio è nei log del server.',
        action: 'Riprova',
      },
    },
  },
} as const;
