// Admin/user-distinction strings (MUSR-01 / D-03/D-26/D-28): the capability grant/revoke
// control, the per-user audit view, and the non-admin fallback the Settings page shows when
// the signed-in identity lacks governance.write.

export const adminEn = {
  admin: {
    notAuthorized: {
      heading: 'Admin access required',
      body: 'This page manages model settings, identities, and audit history. Your identity does not hold the governance capability, so it is hidden. Ask an administrator if you need access.',
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
    access: {
      kicker: 'Access control',
      heading: 'Capabilities',
      body: 'Grant or revoke capabilities per identity. Changes take effect immediately and are recorded server-side. The "*" wildcard is system-managed and cannot be changed here.',
      capabilities: 'Granted capabilities',
      none: 'No capabilities granted.',
      wildcard: 'Full access (system-managed)',
      grantPlaceholder: 'capability.name',
      grant: 'Grant',
      granting: 'Granting...',
      revoke: 'Revoke {{capability}}',
      revoking: 'Revoking...',
      grantError: "Couldn't grant that capability. Check the name and try again.",
      revokeError: "Couldn't revoke that capability. Try again.",
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
  },
} as const;

export const adminIt = {
  admin: {
    notAuthorized: {
      heading: 'Accesso amministratore richiesto',
      body: 'Questa pagina gestisce impostazioni del modello, identità e cronologia di audit. La tua identità non ha la capability di governance, quindi è nascosta. Chiedi a un amministratore se ti serve accesso.',
    },
    removal: {
      title: 'Rimuovere {{name}}?',
      body: "Questa azione elimina definitivamente le conversazioni, la memoria, i file e la chiave OpenRouter di {{name}}. Non si può annullare. OpenRouter mantiene un proprio registro di quanto speso da questa identità; rimuoverla qui non lo elimina.",
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
    access: {
      kicker: 'Controllo accessi',
      heading: 'Capability',
      body: 'Concedi o revoca capability per identità. Le modifiche hanno effetto immediato e vengono registrate lato server. Il wildcard "*" è gestito dal sistema e non è modificabile qui.',
      capabilities: 'Capability concesse',
      none: 'Nessuna capability concessa.',
      wildcard: 'Accesso completo (gestito dal sistema)',
      grantPlaceholder: 'nome.capability',
      grant: 'Concedi',
      granting: 'Concessione...',
      revoke: 'Revoca {{capability}}',
      revoking: 'Revoca in corso...',
      grantError: 'Impossibile concedere quella capability. Controlla il nome e riprova.',
      revokeError: 'Impossibile revocare quella capability. Riprova.',
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
  },
} as const;
