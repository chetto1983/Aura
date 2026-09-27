// The `questionCard.*` bundle: the frame every question the cockpit asks renders in
// (web/src/questions), shared by ask_user and a mounted MCP server's form. Split out of
// resources.ts to keep that file under the 600-LOC cap, the resources.steer.ts precedent.
// Every key exists in BOTH locales; the parity gate fails on any drift.

export const questionCardEn = {
  questionCard: {
    step: 'Step {{current}} of {{total}}',
    progress: 'Form progress',
    approve: 'Approve',
    next: 'Next',
    back: 'Back',
    skip: 'Skip',
    useDefault: 'Use default',
    review: 'Review',
    submit: 'Submit',
    decline: 'Decline',
    yes: 'Yes',
    no: 'No',
    placeholder: 'Type your answer',
    choose: {
      range: 'Choose {{min}} to {{max}}.',
      atLeast: 'Choose at least {{min}}.',
      atMost: 'Choose up to {{max}}.',
    },
    reviewStep: {
      title: 'Review your answers',
      edit: 'Change {{field}}',
      notGiven: 'Not given',
    },
    form: {
      title: 'A form from {{server}}',
      server: 'MCP server {{server}}',
      expiresIn: 'Aura cancels in {{time}}',
    },
    cancel: {
      label: 'Cancel',
      confirm: 'Cancel this request?',
      yes: 'Cancel request',
      no: 'Keep answering',
    },
    receipt: {
      answered: 'Answered.',
      declined: 'Declined.',
      cancelled: 'Cancelled.',
      expired: 'Expired: cancelled automatically.',
    },
    refusal: {
      unrenderable: "Aura declined this form because it can't be shown here.",
      ambiguous_run:
        'Aura declined this form because more than one conversation was using this server.',
    },
    error: {
      required: 'This field is required.',
      invalid: 'This value is not valid.',
      failed: "Couldn't send your answer. Try again.",
      closed: 'This form was already resolved.',
    },
  },
};

export const questionCardIt = {
  questionCard: {
    step: 'Passo {{current}} di {{total}}',
    progress: 'Avanzamento del modulo',
    approve: 'Approva',
    next: 'Avanti',
    back: 'Indietro',
    skip: 'Salta',
    useDefault: 'Usa il predefinito',
    review: 'Rivedi',
    submit: 'Invia',
    decline: 'Rifiuta',
    yes: 'Sì',
    no: 'No',
    placeholder: 'Scrivi la tua risposta',
    choose: {
      range: 'Scegline da {{min}} a {{max}}.',
      atLeast: 'Scegline almeno {{min}}.',
      atMost: 'Scegline al massimo {{max}}.',
    },
    reviewStep: {
      title: 'Rivedi le risposte',
      edit: 'Modifica {{field}}',
      notGiven: 'Non indicato',
    },
    form: {
      title: 'Un modulo da {{server}}',
      server: 'Server MCP {{server}}',
      expiresIn: 'Aura annulla tra {{time}}',
    },
    cancel: {
      label: 'Annulla',
      confirm: 'Annullare questa richiesta?',
      yes: 'Annulla richiesta',
      no: 'Continua a rispondere',
    },
    receipt: {
      answered: 'Risposto.',
      declined: 'Rifiutato.',
      cancelled: 'Annullato.',
      expired: 'Scaduto: annullato automaticamente.',
    },
    refusal: {
      unrenderable: 'Aura ha rifiutato questo modulo perché qui non si può mostrare.',
      ambiguous_run:
        'Aura ha rifiutato questo modulo perché più di una conversazione stava usando questo server.',
    },
    error: {
      required: 'Questo campo è obbligatorio.',
      invalid: 'Questo valore non è valido.',
      failed: 'Impossibile inviare la risposta. Riprova.',
      closed: 'Questo modulo è già stato risolto.',
    },
  },
};
