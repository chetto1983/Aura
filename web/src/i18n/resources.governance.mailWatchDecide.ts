// resources.governance.mailWatchDecide.ts holds the labels' decision-model copy (EN + IT), spread
// into governance.mailWatch.decide. The model reads only a label's description, so the copy says so.

export const mailWatchDecideEn = {
  description: 'Description',
  descriptionHint: 'The model reads only this: say who writes and about what.',
  keep: 'Keep',
  discard: 'Discard',
  discardHint: 'Discarded mail leaves the list, and the digest counts it.',
  noDescription: 'Without a description the model skips it',
  reserved: 'Reserved name: the model skips it',
  decisions: { discarded: 'Discarded' },
  row: { discarded: 'Discarded' },
  cloud: {
    label: 'Let the OpenRouter decision model read this account',
    hint: 'The subject and start of each mail go to OpenRouter, sent with a request not to keep them.',
  },
  failing: 'The model has not answered since {{time}} ({{reason}})',
  error: {
    no_credential: 'no OpenRouter key is set',
    unreachable: 'it could not be reached',
    loading: 'it is still loading',
    rejected: 'OpenRouter refused the request',
    upstream: 'its server failed',
    malformed: 'its answer could not be read',
    timeout: 'it took too long',
  },
};

export const mailWatchDecideIt = {
  description: 'Descrizione',
  descriptionHint: 'Il modello legge solo questa: scrivi chi ti scrive e di cosa.',
  keep: 'Tieni',
  discard: 'Scarta',
  discardHint: 'Le mail scartate escono dalla lista e il riepilogo le conta.',
  noDescription: 'Senza descrizione il modello non la usa',
  reserved: 'Nome riservato: il modello non la usa',
  decisions: { discarded: 'Scartate' },
  row: { discarded: 'Scartata' },
  cloud: {
    label: 'Lascia leggere questo account al modello su OpenRouter',
    hint: 'Oggetto e inizio di ogni mail vanno a OpenRouter, che li riceve con la richiesta di non conservarli.',
  },
  failing: 'Il modello non risponde dalle {{time}} ({{reason}})',
  error: {
    no_credential: 'manca la chiave OpenRouter',
    unreachable: 'non si riesce a raggiungerlo',
    loading: 'si sta ancora caricando',
    rejected: 'OpenRouter ha rifiutato la richiesta',
    upstream: 'il suo server ha avuto un errore',
    malformed: 'la risposta non era leggibile',
    timeout: 'ci ha messo troppo',
  },
};
