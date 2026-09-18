import type { TFunction } from 'i18next';

// studioErrors.ts — one sentence per refusal, in one place, because the same failure reaches
// the operator twice: as the alert over a refused submission and as the body of a failed
// record in the history.
//
// Every code the design's A2 tables name is here, in both locales. Falling through to the
// server's own message translated nothing: that string is written in English by Go, so an
// Italian operator read an English sentence for a failure Aura already knows how to name.
// The fallback stays for a code the server mints after this build — there IS no sentence for
// one of those, and the server's own words beat an invented paraphrase.

export function studioErrorSentence(t: TFunction, code: string, message: string): string {
  switch (code) {
    case 'no_key':
      return t('studio.error.no_key');
    case 'no_credit':
      return t('studio.error.no_credit');
    case 'unsupported':
      return t('studio.error.unsupported');
    case 'model_rejected':
      return t('studio.error.model_rejected');
    case 'content_blocked':
      return t('studio.error.content_blocked');
    case 'asset_not_found':
      return t('studio.error.asset_not_found');
    case 'too_large':
      return t('studio.error.too_large');
    case 'job_failed':
      return t('studio.error.job_failed');
    case 'job_expired':
      return t('studio.error.job_expired');
    case 'outcome_unknown':
      return t('studio.error.outcome_unknown');
    case 'local_route':
      return t('studio.error.local_route');
    default:
      return message.trim() === '' ? t('studio.error.generic') : message;
  }
}
