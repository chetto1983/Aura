import type { TFunction } from 'i18next';

// studioErrors.ts — one sentence per refusal, in one place, because the same failure reaches
// the operator twice: as the alert over a refused submission and as the body of a failed
// record in the history.
//
// Only the codes with a localized sentence are translated. A code the server mints later has
// none, and the server's own message is shown instead of a made-up one; the generic fallback
// exists for the case where there is no message either, and says only what is true.

export function studioErrorSentence(t: TFunction, code: string, message: string): string {
  switch (code) {
    case 'no_key':
      return t('studio.error.no_key');
    case 'no_credit':
      return t('studio.error.no_credit');
    case 'outcome_unknown':
      return t('studio.error.outcome_unknown');
    case 'local_route':
      return t('studio.error.local_route');
    default:
      return message.trim() === '' ? t('studio.error.generic') : message;
  }
}
