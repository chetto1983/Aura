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
};

export const mailWatchDecideIt = {
  description: 'Descrizione',
  descriptionHint: 'Il modello legge solo questa: scrivi chi ti scrive e di cosa.',
  keep: 'Tieni',
  discard: 'Scarta',
  discardHint: 'Le mail scartate escono dalla lista e il riepilogo le conta.',
  noDescription: 'Senza descrizione il modello non la usa',
  reserved: 'Nome riservato: il modello non la usa',
};
