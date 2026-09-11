// Admin/user-distinction strings (MUSR-01 / D-03/D-26/D-28): the capability grant/revoke
// control, the per-user audit view, and the non-admin fallback the Settings page shows when
// the signed-in identity lacks governance.write.

export const adminEn = {
  admin: {
    notAuthorized: {
      heading: 'Admin access required',
      body: 'This page manages model settings, identities, and audit history. Your identity does not hold the governance capability, so it is hidden. Ask an administrator if you need access.',
    },
    roster: {
      kicker: 'Access control',
      heading: 'Identities',
      body: 'Everyone here holds the same tool access (RBAC-03); the only difference an admin has is creating and removing identities and setting their spending cap.',
      adminBadge: 'Admin',
      memberBadge: 'Member',
      removeAriaLabel: 'Remove {{name}}',
      cannotRemoveSelf:
        "The administrator account can't remove itself — use `aura identity` on the host.",
    },
    removal: {
      title: 'Remove {{name}}?',
      body: "This permanently deletes {{name}}'s conversations, memory, files and OpenRouter key. This can't be undone. OpenRouter keeps its own record of what this identity spent; removing it here does not erase that.",
      confirmLabel: 'Type {{email}} to confirm',
      confirm: 'Remove permanently',
      cancel: 'Cancel',
      inFlight: 'Removing {{name}}…',
      partialFailure:
        "Couldn't finish removing {{name}}. The removal is safe to retry — some data may already be gone.",
    },
    reviewStep: {
      accessLabel: 'Access',
      accessBody:
        "Full access to Aura's tools — mounting MCP servers, authoring skills, running sandboxed commands and approving actions. Only user management stays admin-only.",
      creditLabel: 'Starting credit',
      creditBody: "$0.00 — this identity can't run a turn until you add credit after creating it.",
    },
    credit: {
      heading: 'Credit',
      toggleShow: 'Show credit for {{name}}',
      toggleHide: 'Hide credit for {{name}}',
      loading: 'Loading credit...',
      loadError: "Couldn't load this identity's credit. Try again.",
      capLabel: 'Spending cap',
      resetLabel: 'Reset interval',
      interval: {
        daily: 'Daily',
        weekly: 'Weekly',
        monthly: 'Monthly',
      },
      spendLabel: 'Spend',
      remainingLabel: 'Remaining',
      noLimit: 'No limit',
      noLimitBody: "An administrator's own key has no spending cap.",
      noKeyHeading: 'No OpenRouter key yet',
      noKeyCause: {
        management_key_unset:
          'An admin connects OpenRouter first: the management key goes in the first-run setup or in Settings.',
        minting_unavailable: "This deployment can't mint OpenRouter keys.",
        not_minted:
          "Aura hasn't minted it yet. It retries when Aura restarts or when the OpenRouter settings are saved; the daemon log names the provider's error.",
      },
      gaugeValue: '{{spend}} / {{cap}} · {{percent}}%',
      gaugeLabel: 'Spend against the cap; warns at {{near}}% and again at {{critical}}%',
      saveCap: 'Save cap',
      emptyHeading: 'No spending cap to show',
      emptyBody:
        "This deployment runs on a local model backend, which doesn't bill — there's no cap or spend to show.",
      saveError: "Couldn't update the spending cap. Check the amount and try again.",
      latencyUp: 'Takes about 25 seconds to apply.',
      latencyDown: 'Takes about 5 seconds to apply.',
      exhaustedRefusal:
        '{{name}} has no remaining credit for this turn. Ask an administrator to add credit under Settings → Identities.',
      turnErrorGeneric: 'Something went wrong with this turn. Try again.',
    },
    identity: {
      label: 'Identity',
      loading: 'Loading identities...',
      error: "Couldn't load identities. Check the server and try again.",
      empty: 'No identities yet.',
      you: 'you',
    },
    audit: {
      kicker: 'Audit',
      heading: 'Activity',
      body: 'Recent MCP, skill, and tool activity for the selected identity, newest first.',
      loading: 'Loading activity...',
      error: "Couldn't load activity. Try again.",
      empty: 'No recorded activity for this identity yet.',
      refresh: 'Refresh',
      prev: 'Previous',
      next: 'Next',
      selectPrompt: 'Select an identity to view its activity.',
      source: {
        mcp: 'MCP',
        skill: 'Skill',
        tool: 'Tool',
      },
      outcome: {
        ok: 'ok',
        error: 'error',
        running: 'running',
      },
    },
    overview: {
      heading: 'Spend overview',
      loading: 'Loading spend overview...',
      loadError: "Couldn't load the spend overview. Try refreshing.",
      empty: "No spend yet — this account hasn't made a billed request.",
      overAllocation:
        "Assigned caps total more than this account's available OpenRouter credit. A lower-priority identity could be starved without warning — lower a cap or add credit to the account.",
      uncappedKeys: 'Keys with no limit, which draw on the same credit: {{keys}}.',
      vsPrevPeriod: 'vs prev period',
      kpi: {
        totalSpend: 'Total spend',
        requests: 'Requests',
        tokenVolume: 'Token volume',
        cacheHitRate: 'Cache hit rate',
        blendedCost: 'Blended $/1M',
      },
      topIdentities: {
        heading: 'Top identities by spend',
        lifetimeSpend: 'Lifetime spend',
        seeFullRoster: 'See full roster below',
      },
    },
  },
} as const;

export const adminIt = {
  admin: {
    notAuthorized: {
      heading: 'Accesso amministratore richiesto',
      body: 'Questa pagina gestisce impostazioni del modello, identità e cronologia di audit. La tua identità non ha la capability di governance, quindi è nascosta. Chiedi a un amministratore se ti serve accesso.',
    },
    roster: {
      kicker: 'Controllo accessi',
      heading: 'Identità',
      body: "Qui tutti hanno lo stesso accesso agli strumenti (RBAC-03); l'unica differenza di un amministratore è creare e rimuovere identità e impostarne il limite di spesa.",
      adminBadge: 'Admin',
      memberBadge: 'Membro',
      removeAriaLabel: 'Rimuovi {{name}}',
      cannotRemoveSelf:
        "L'account amministratore non può rimuovere se stesso — usa `aura identity` sull'host.",
    },
    removal: {
      title: 'Rimuovere {{name}}?',
      body: 'Questa azione elimina definitivamente le conversazioni, la memoria, i file e la chiave OpenRouter di {{name}}. Non si può annullare. OpenRouter mantiene un proprio registro di quanto speso da questa identità; rimuoverla qui non lo elimina.',
      confirmLabel: 'Digita {{email}} per confermare',
      confirm: 'Rimuovi definitivamente',
      cancel: 'Annulla',
      inFlight: 'Rimozione di {{name}}…',
      partialFailure:
        'Impossibile completare la rimozione di {{name}}. La rimozione è sicura da ripetere — alcuni dati potrebbero già essere spariti.',
    },
    reviewStep: {
      accessLabel: 'Accesso',
      accessBody:
        "Accesso completo agli strumenti di Aura — montare server MCP, creare skill, eseguire comandi in sandbox e approvare azioni. Solo la gestione utenti resta riservata all'amministratore.",
      creditLabel: 'Credito iniziale',
      creditBody:
        '$0.00 — questa identità non può eseguire un turno finché non aggiungi credito dopo la creazione.',
    },
    credit: {
      heading: 'Credito',
      toggleShow: 'Mostra il credito di {{name}}',
      toggleHide: 'Nascondi il credito di {{name}}',
      loading: 'Caricamento credito...',
      loadError: 'Impossibile caricare il credito di questa identità. Riprova.',
      capLabel: 'Limite di spesa',
      resetLabel: 'Intervallo di reset',
      interval: {
        daily: 'Giornaliero',
        weekly: 'Settimanale',
        monthly: 'Mensile',
      },
      spendLabel: 'Speso',
      remainingLabel: 'Residuo',
      noLimit: 'Nessun limite',
      noLimitBody: 'La chiave di un amministratore non ha un limite di spesa.',
      noKeyHeading: 'Ancora nessuna chiave OpenRouter',
      noKeyCause: {
        management_key_unset:
          'Prima un amministratore collega OpenRouter: la chiave di gestione va nella configurazione iniziale o nelle Impostazioni.',
        minting_unavailable: 'Questa installazione non può creare chiavi OpenRouter.',
        not_minted:
          "Aura non l'ha ancora creata. Ci riprova al riavvio o quando si salvano le impostazioni di OpenRouter; il log del demone riporta l'errore del provider.",
      },
      gaugeValue: '{{spend}} / {{cap}} · {{percent}}%',
      gaugeLabel: 'Spesa rispetto al limite; avvisa al {{near}}% e di nuovo al {{critical}}%',
      saveCap: 'Salva limite',
      emptyHeading: 'Nessun limite di spesa da mostrare',
      emptyBody:
        'Questa installazione usa un backend a modello locale, che non fattura — non c’è un limite o una spesa da mostrare.',
      saveError: 'Impossibile aggiornare il limite di spesa. Controlla l’importo e riprova.',
      latencyUp: 'Richiede circa 25 secondi per applicarsi.',
      latencyDown: 'Richiede circa 5 secondi per applicarsi.',
      exhaustedRefusal:
        '{{name}} non ha credito residuo per questo turno. Chiedi a un amministratore di aggiungere credito in Impostazioni → Identità.',
      turnErrorGeneric: 'Qualcosa è andato storto con questo turno. Riprova.',
    },
    identity: {
      label: 'Identità',
      loading: 'Caricamento identità...',
      error: 'Impossibile caricare le identità. Controlla il server e riprova.',
      empty: 'Ancora nessuna identità.',
      you: 'tu',
    },
    audit: {
      kicker: 'Audit',
      heading: 'Attività',
      body: "Attività recenti di MCP, skill e tool per l'identità selezionata, dalla più recente.",
      loading: 'Caricamento attività...',
      error: 'Impossibile caricare le attività. Riprova.',
      empty: 'Ancora nessuna attività registrata per questa identità.',
      refresh: 'Aggiorna',
      prev: 'Precedente',
      next: 'Successivo',
      selectPrompt: "Seleziona un'identità per vederne l'attività.",
      source: {
        mcp: 'MCP',
        skill: 'Skill',
        tool: 'Tool',
      },
      outcome: {
        ok: 'ok',
        error: 'errore',
        running: 'in corso',
      },
    },
    overview: {
      heading: 'Panoramica della spesa',
      loading: 'Caricamento della panoramica della spesa...',
      loadError: 'Impossibile caricare la panoramica della spesa. Prova ad aggiornare.',
      empty:
        'Ancora nessuna spesa — questo account non ha ancora effettuato una richiesta fatturata.',
      overAllocation:
        "I limiti assegnati superano nel totale il credito OpenRouter disponibile per questo account. Un'identità a priorità più bassa potrebbe restare senza credito senza preavviso — riduci un limite o aggiungi credito all'account.",
      uncappedKeys: 'Chiavi senza limite, che attingono allo stesso credito: {{keys}}.',
      vsPrevPeriod: 'rispetto al periodo precedente',
      kpi: {
        totalSpend: 'Spesa totale',
        requests: 'Richieste',
        tokenVolume: 'Volume token',
        cacheHitRate: 'Tasso di cache hit',
        blendedCost: '$/1M combinato',
      },
      topIdentities: {
        heading: 'Prime identità per spesa',
        lifetimeSpend: 'Spesa complessiva',
        seeFullRoster: 'Vedi il registro completo qui sotto',
      },
    },
  },
} as const;
