export const footerEn = {
  footer: {
    runtimeLabel: 'Runtime telemetry',
    tokens: 'Tokens',
    cache: 'Cache',
    cost: 'Cost',
    context: 'Context',
    perTurn: 'This turn',
    session: 'Session',
    none: '—',
    noSpend: 'Local reply - no model spend',
    showDetails: 'Show telemetry details',
    hideDetails: 'Hide telemetry details',
    settledAnnouncement:
      'Run complete. Tokens {{tokens}}, cache {{cache}}, cost {{cost}}. Session tokens {{sessionTokens}}, cache {{sessionCache}}, cost {{sessionCost}}.',
    gaugeValue: '{{used}} / {{window}} · {{percent}}%',
    contextLabel: 'Context budget - near at {{near}}%, critical at {{critical}}%',
    compacted: 'Compacted {{count}} older turns',
    compactionFailed: 'Compaction failed; fallback dropped {{count}} older turns',
  },
};

export const footerIt = {
  footer: {
    runtimeLabel: 'Telemetria runtime',
    tokens: 'Token',
    cache: 'Cache',
    cost: 'Costo',
    context: 'Contesto',
    perTurn: 'Questo turno',
    session: 'Sessione',
    none: '—',
    noSpend: 'Risposta locale - nessun costo modello',
    showDetails: 'Mostra dettagli telemetria',
    hideDetails: 'Nascondi dettagli telemetria',
    settledAnnouncement:
      'Esecuzione completata. Token {{tokens}}, cache {{cache}}, costo {{cost}}. Token sessione {{sessionTokens}}, cache {{sessionCache}}, costo {{sessionCost}}.',
    gaugeValue: '{{used}} / {{window}} · {{percent}}%',
    contextLabel: 'Budget di contesto - attenzione a {{near}}%, critico a {{critical}}%',
    compacted: 'Compattati {{count}} turni più vecchi',
    compactionFailed: 'Compattazione fallita; il fallback ha eliminato {{count}} turni più vecchi',
  },
};
