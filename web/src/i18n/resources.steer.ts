// The `chat.steer.*` i18n bundle (Phase 52 plan 07, D-10's composer contract) — split out of
// resources.ts to keep that file under the 600-LOC cap (CLAUDE.md "no god class"), the
// resources.composer.ts / resources.governance.ts / resources.graph.ts precedent. Nested
// under `chat.steer` in each locale exactly like resources.composer.ts's `effort` bundle is
// nested under `chat.composer.effort`. Every key exists in BOTH locales — the parity gate
// (src/i18n/__tests__/resources.parity.test.ts) fails on any drift.

export const chatSteerEn = {
  sendAria: 'Redirect the current turn',
  notice: {
    redirected: 'Message sent — the turn was redirected.',
    autoDelivered: 'Delivered as a new message once the previous turn ended.',
  },
  refusal: {
    invalid: "That message couldn't be redirected — try a shorter one.",
    busy: 'Aura already has a redirect queued. Wait a moment and try again.',
    ended: 'The turn already finished — send this as a normal message.',
    failed: "Couldn't redirect the turn. Try again.",
  },
};

export const chatSteerIt = {
  sendAria: 'Reindirizza il turno in corso',
  notice: {
    redirected: 'Messaggio inviato — il turno è stato reindirizzato.',
    autoDelivered: 'Consegnato come nuovo messaggio perché il turno precedente si è concluso.',
  },
  refusal: {
    invalid: 'Non è stato possibile reindirizzare questo messaggio — provane uno più breve.',
    busy: 'Aura ha già un reindirizzamento in coda. Attendi un momento e riprova.',
    ended: 'Il turno è già terminato — invia questo messaggio normalmente.',
    failed: 'Impossibile reindirizzare il turno. Riprova.',
  },
};
