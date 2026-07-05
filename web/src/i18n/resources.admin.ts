// Admin/user-distinction strings (MUSR-01 / D-03/D-26/D-28): the capability grant/revoke
// control, the per-user audit view, and the non-admin fallback the Settings page shows when
// the signed-in identity lacks governance.write.

export const adminEn = {
  admin: {
    notAuthorized: {
      heading: 'Admin access required',
      body: 'This page manages model settings, identities, and audit history. Your identity does not hold the governance capability, so it is hidden. Ask an administrator if you need access.',
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
    },
  },
} as const;

export const adminIt = {
  admin: {
    notAuthorized: {
      heading: 'Accesso amministratore richiesto',
      body: 'Questa pagina gestisce impostazioni del modello, identità e cronologia di audit. La tua identità non ha la capability di governance, quindi è nascosta. Chiedi a un amministratore se ti serve accesso.',
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
      body: 'Attività recenti di MCP, skill e tool per l\'identità selezionata, dalla più recente.',
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
    },
  },
} as const;
