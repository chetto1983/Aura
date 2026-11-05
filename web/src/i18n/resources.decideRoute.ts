// The decision model's route control (Models pane): off, the local sidecar, or an OpenRouter model.

export const decideRouteEn = {
  decideRoute: {
    heading: 'Mail decision model',
    body: 'Labels unsorted mail from its subject and first lines. It never reads the sender. The choice applies from the next mail check, with no restart.',
    label: 'Decision model route',
    model: 'Decision model',
    off: 'Off',
    local: 'Local',
    openrouter: 'OpenRouter',
    localMissing: 'Local model not installed on this appliance',
    localCaption:
      'd1-3B by Liquid AI, LFM 1.0 licence: commercial use is granted under 10 million dollars of annual revenue',
    licence: 'Read the licence',
    useLocal: 'Use the local model',
    useOff: 'Turn the decision model off',
    openrouterCaption:
      'The subject and the start of each mail go to OpenRouter, only for accounts that allow it.',
  },
};

export const decideRouteIt = {
  decideRoute: {
    heading: 'Modello di decisione per la posta',
    body: 'Etichetta la posta non ancora smistata dall’oggetto e dalle prime righe. Non legge mai il mittente. La scelta vale dal prossimo controllo della posta, senza riavvio.',
    label: 'Percorso del modello di decisione',
    model: 'Modello di decisione',
    off: 'Spento',
    local: 'Locale',
    openrouter: 'OpenRouter',
    localMissing: 'Modello locale non installato su questo apparecchio',
    localCaption:
      'd1-3B di Liquid AI, licenza LFM 1.0: uso commerciale concesso sotto i 10 milioni di dollari di fatturato annuo',
    licence: 'Leggi la licenza',
    useLocal: 'Usa il modello locale',
    useOff: 'Spegni il modello di decisione',
    openrouterCaption:
      'L’oggetto e l’inizio delle mail vanno a OpenRouter, solo per gli account che lo permettono.',
  },
};
