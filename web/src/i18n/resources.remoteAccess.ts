export const remoteAccessEn = {
  remoteAccess: {
    heading: 'Remote access',
    body: 'Publish Aura through a Cloudflare named tunnel while keeping direct access available for operators.',
    loading: 'Loading remote access…',
    loadError: 'Could not load remote access. Check the server and try again.',
    retry: 'Retry',
    steps: {
      label: 'Remote access setup progress',
      account: 'Account',
      domain: 'Domain',
      nameservers: 'Nameservers',
      tunnel: 'Tunnel',
      access: 'Access',
      warp: 'WARP',
      verify: 'Verify',
    },
    token: {
      label: 'Cloudflare API token',
      hint: 'The token is verified before Aura stores it. It is never shown again.',
      verify: 'Verify token',
      verified: 'Token verified. Select the Cloudflare account to use.',
      error: 'The token could not be verified. Replace it and try again.',
    },
    domain: {
      account: 'Cloudflare account',
      domain: 'Registered domain',
      publicLabel: 'Public hostname label',
      warpLabel: 'WARP hostname label',
      external:
        'Buy or register the domain with a registrar first. Aura cannot purchase or change it for you.',
      quickTunnel:
        'Quick Tunnels are temporary testing tools and cannot carry Aura production SSE traffic.',
      preview: 'Published URLs',
      save: 'Save and start setup',
      prerequisite: 'A registered domain is required before remote access can be configured.',
    },
    nameservers: {
      heading: 'Point your registrar at Cloudflare',
      body: 'Replace the domain nameservers at your registrar, then Aura will continue when the zone becomes active.',
      waiting: 'Waiting for Cloudflare to activate the zone…',
    },
    hostnames: { public: 'Public Access hostname', warp: 'WARP-required hostname' },
    verify: {
      heading: 'Verify the external path',
      body: 'Open the public hostname, complete Cloudflare Access one-time PIN and Authula, then confirm from that external cockpit.',
      open: 'Open public hostname',
      externalOnly:
        'This confirmation is available only from the configured public hostname after Cloudflare Access and Authula.',
      accept: 'Confirm external access',
      accepted: 'External access confirmation requested.',
    },
    status: {
      heading: 'Remote access status',
      phase: 'Reconciliation phase',
      connector: 'Connector',
      membership:
        'Access membership is reconciled from active Aura users; administrators remain protected from lockout.',
      direct:
        'Direct IP or LAN ingress bypasses Cloudflare Access and remains protected by Authula.',
      lastError: 'Remote access needs attention. Retry or replace the API token.',
      accepted: 'Action accepted. Refreshing status…',
      actionError: 'The remote access action was not accepted. Refresh the status and try again.',
      failedDeletion:
        'Deletion is still in progress or needs attention. Aura remains disabled until the owned resources are removed.',
      lastReconciled: 'Last reconciled {{time}}',
      phases: {
        disabled: 'Disabled',
        validating: 'Validating configuration',
        waiting_nameservers: 'Waiting for nameservers',
        provisioning: 'Provisioning Cloudflare resources',
        connecting: 'Waiting for external acceptance',
        healthy: 'Healthy',
        degraded: 'Degraded',
        error: 'Needs attention',
        deleting: 'Deleting resources',
      },
      connectors: {
        healthy: 'Healthy',
        disconnected: 'Disconnected',
        awaiting_external_acceptance: 'Awaiting external acceptance',
      },
    },
    actions: {
      reconcile: 'Retry reconciliation',
      rotate: 'Rotate token in Cloudflare Dashboard',
      refresh: 'Refresh Dashboard-rotated token',
      disable: 'Disable remote access',
      reenable: 'Re-enable remote access',
      delete: 'Delete remote access',
      deleteForever: 'Delete permanently',
      cancel: 'Cancel',
    },
    rotation: {
      body: 'Cloudflare does not expose a token-rotation API. Rotate the tunnel token manually in the Cloudflare Dashboard, then refresh Aura’s projected token.',
      documentation: 'Open Cloudflare token instructions',
    },
    permissions: {
      heading: 'API token permissions',
      body: 'Create a scoped Cloudflare API token with: Account Settings Read; Zone Read and Zone Write; Cloudflare Tunnel/Connector Write; DNS Write; Access Organizations/Identity Providers/Groups Write; Access Apps/Policies Write; and Zero Trust Write.',
      documentation: 'Open Cloudflare API token creation',
    },
    warp: {
      heading: 'WARP organization enrollment',
      body: 'The WARP hostname requires the Cloudflare One client enrolled in this same organization through Gateway. Aura creates no private LAN, CIDR, or Docker route.',
      documentation: 'Open Gateway enrollment guidance',
      dependency:
        'Enroll the Cloudflare One client in the same organization. Aura cannot open an organization-specific enrollment link today.',
    },
    delete: {
      title: 'Delete Cloudflare remote access?',
      body: 'This removes Aura-owned Cloudflare resources. Type the exact current public hostname to continue.',
      label: 'Type {{hostname}} to confirm',
    },
  },
} as const;

export const remoteAccessIt = {
  remoteAccess: {
    heading: 'Accesso remoto',
    body: 'Pubblica Aura tramite un tunnel Cloudflare con nome, mantenendo disponibile l’accesso diretto per gli operatori.',
    loading: 'Caricamento accesso remoto…',
    loadError: 'Impossibile caricare l’accesso remoto. Controlla il server e riprova.',
    retry: 'Riprova',
    steps: {
      label: 'Avanzamento configurazione accesso remoto',
      account: 'Account',
      domain: 'Dominio',
      nameservers: 'Nameserver',
      tunnel: 'Tunnel',
      access: 'Access',
      warp: 'WARP',
      verify: 'Verifica',
    },
    token: {
      label: 'Token API Cloudflare',
      hint: 'Il token viene verificato prima che Aura lo salvi. Non viene mai mostrato di nuovo.',
      verify: 'Verifica token',
      verified: 'Token verificato. Seleziona l’account Cloudflare da usare.',
      error: 'Impossibile verificare il token. Sostituiscilo e riprova.',
    },
    domain: {
      account: 'Account Cloudflare',
      domain: 'Dominio registrato',
      publicLabel: 'Etichetta hostname pubblico',
      warpLabel: 'Etichetta hostname WARP',
      external:
        'Acquista o registra prima il dominio presso un registrar. Aura non puo acquistarlo o modificarlo per te.',
      quickTunnel:
        'I Quick Tunnel sono strumenti temporanei di test e non supportano il traffico SSE di produzione di Aura.',
      preview: 'URL pubblicati',
      save: 'Salva e avvia configurazione',
      prerequisite: 'Per configurare l’accesso remoto serve un dominio registrato.',
    },
    nameservers: {
      heading: 'Indica Cloudflare al registrar',
      body: 'Sostituisci i nameserver del dominio presso il registrar; Aura continua quando la zona diventa attiva.',
      waiting: 'In attesa che Cloudflare attivi la zona…',
    },
    hostnames: { public: 'Hostname Access pubblico', warp: 'Hostname con WARP obbligatorio' },
    verify: {
      heading: 'Verifica il percorso esterno',
      body: 'Apri l’hostname pubblico, completa il PIN monouso Cloudflare Access e Authula, poi conferma da quel cockpit esterno.',
      open: 'Apri hostname pubblico',
      externalOnly:
        'Questa conferma e disponibile solo dall’hostname pubblico configurato dopo Cloudflare Access e Authula.',
      accept: 'Conferma accesso esterno',
      accepted: 'Richiesta di conferma dell’accesso esterno inviata.',
    },
    status: {
      heading: 'Stato accesso remoto',
      phase: 'Fase di riconciliazione',
      connector: 'Connettore',
      membership:
        'La membership Access viene riconciliata dagli utenti Aura attivi; gli amministratori restano protetti dal lockout.',
      direct:
        'L’ingresso diretto da IP o LAN ignora Cloudflare Access e resta protetto da Authula.',
      lastError: 'L’accesso remoto richiede attenzione. Riprova o sostituisci il token API.',
      accepted: 'Azione accettata. Aggiornamento stato…',
      actionError: 'L’azione di accesso remoto non e stata accettata. Aggiorna lo stato e riprova.',
      failedDeletion:
        'L’eliminazione e ancora in corso o richiede attenzione. Aura resta disabilitata finche le risorse possedute non vengono rimosse.',
      lastReconciled: 'Ultima riconciliazione {{time}}',
      phases: {
        disabled: 'Disabilitato',
        validating: 'Configurazione in verifica',
        waiting_nameservers: 'In attesa dei nameserver',
        provisioning: 'Risorse Cloudflare in preparazione',
        connecting: 'In attesa conferma esterna',
        healthy: 'Sano',
        degraded: 'Degradato',
        error: 'Richiede attenzione',
        deleting: 'Eliminazione risorse',
      },
      connectors: {
        healthy: 'Sano',
        disconnected: 'Disconnesso',
        awaiting_external_acceptance: 'In attesa conferma esterna',
      },
    },
    actions: {
      reconcile: 'Riprova riconciliazione',
      rotate: 'Ruota token nel Dashboard Cloudflare',
      refresh: 'Aggiorna token ruotato nel Dashboard',
      disable: 'Disabilita accesso remoto',
      reenable: 'Riattiva accesso remoto',
      delete: 'Elimina accesso remoto',
      deleteForever: 'Elimina definitivamente',
      cancel: 'Annulla',
    },
    rotation: {
      body: 'Cloudflare non espone un’API per ruotare il token. Ruota manualmente il token tunnel nel Dashboard Cloudflare, poi aggiorna il token proiettato da Aura.',
      documentation: 'Apri istruzioni token Cloudflare',
    },
    permissions: {
      heading: 'Permessi token API',
      body: 'Crea un token API Cloudflare con scope: Account Settings Read; Zone Read e Zone Write; Cloudflare Tunnel/Connector Write; DNS Write; Access Organizations/Identity Providers/Groups Write; Access Apps/Policies Write; e Zero Trust Write.',
      documentation: 'Apri creazione token API Cloudflare',
    },
    warp: {
      heading: 'Registrazione organizzazione WARP',
      body: 'L’hostname WARP richiede il client Cloudflare One registrato nella stessa organizzazione tramite Gateway. Aura non crea route LAN private, CIDR o Docker.',
      documentation: 'Apri guida registrazione Gateway',
      dependency:
        'Registra il client Cloudflare One nella stessa organizzazione. Aura non puo aprire oggi un link di registrazione specifico dell’organizzazione.',
    },
    delete: {
      title: 'Eliminare l’accesso remoto Cloudflare?',
      body: 'Rimuove le risorse Cloudflare possedute da Aura. Scrivi l’hostname pubblico corrente esatto per continuare.',
      label: 'Scrivi {{hostname}} per confermare',
    },
  },
} as const;
