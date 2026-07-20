// resources.share.ts — the R-03 i18n module for Phase 37F (conversation/artifact sharing).
// One top-level `share` namespace so `t('share.modal.title')` resolves; every leaf key exists
// in BOTH shareEn and shareIt (proven by the recursive key-tree parity test in
// resources.share.test.ts), wired into resources.ts in exactly 4 LOC (one import, one spread
// per language block) rather than inlined — resources.ts was at 576/600 LOC at plan time and
// has almost no headroom left.
//
// A note for the reviewer, since it looks like it might conflict but does not: CLAUDE.md
// mandates English-only for PROMPT/LLM-FACING overlays (the model never sees Italian). These
// are USER-FACING UI strings, which the i18n rule requires in both en and it like every other
// domain module (resources.governance.ts, resources.display.ts, …). The two rules govern
// different surfaces and do not conflict.
//
// The public share page NEVER receives the owner's language: it falls back to the browser's
// Accept-Language via the i18next detector default. Persisting the owner's language into the
// snapshot would be a fingerprinting-adjacent leak and a needless coupling (RESEARCH §6) — no
// key here is ever written into a Snapshot.
export const shareEn = {
  share: {
    toggle: {
      label: 'Share conversation',
    },
    modal: {
      title: 'Share conversation',
      tier: {
        legend: 'Who can open this link',
        internal: {
          label: 'Internal link',
          description: 'Anyone with the link and an Aura account',
        },
        public: {
          label: 'Public link',
          description: 'Anyone with the link, even without an Aura account',
        },
      },
      // The ChatGPT honesty note, shown ONLY when the public tier is selected (a warning shown
      // always is a warning nobody reads) and at MINT time, not only at revoke — that is when
      // the decision is actually made (RESEARCH §2).
      publicWarning:
        'A public link is visible to anyone who receives it. Revoking it prevents new access, but does not delete copies already seen or cached by search engines.',
      expiry: {
        legend: 'Expiry',
        '1d': '1 day',
        '7d': '7 days',
        '30d': '30 days',
        custom: 'Custom',
        customDaysLabel: 'Custom expiry (days)',
      },
      frozenNote:
        'The snapshot is frozen now — messages sent after this point will not appear in this link.',
      cancel: 'Cancel',
      create: 'Create link',
    },
    shared: {
      urlLabel: 'Link',
      copy: 'Copy',
      copied: 'Copied ✓',
      meta: '{{tier}} · expires in {{expiry}} · created {{created}}',
      // The internal tier carries no expires_at at all (D-01: only public links expire) — a
      // SEPARATE template, not `meta` with an empty/placeholder {{expiry}}, because "expires in
      // no expiry" is not a sentence in either language.
      metaNoExpiry: '{{tier}} · created {{created}}',
      // The bare "{{expiry}}" fragment meta's template above interpolates — kept separate from
      // settings.expiresIn (a full sentence used by the OTHER management surface, 37F-17) so
      // meta never doubles up "expires in expires in N days".
      expiresInDays: '{{count}} days',
      // Aura's differentiator (RESEARCH §2): the data already exists (conversations.last_active_at
      // vs shared_links.updated_at), so surfacing it costs no new storage.
      stale: '{{count}} new messages are not in this link',
      update: 'Update',
      revoke: 'Revoke link',
    },
    // Announced via aria-live after a successful revoke (RESEARCH §5); the modal then returns
    // to the tier-selection form (D-01: the modal never dead-ends on a revoked screen).
    revoked: {
      announcement: 'Link revoked',
    },
    revokeConfirm: {
      title: 'Revoke this link?',
      body: 'Anyone holding this link will immediately lose access. This cannot be undone.',
      confirm: 'Revoke',
    },
    section: {
      heading: 'Shared',
      empty: 'This conversation has not been shared.',
    },
    settings: {
      heading: 'Shared links',
      empty: 'You have no shared conversations.',
      // The global list's own load-error copy (a query-level failure, distinct from a
      // per-link revoke failure below) — scoped here rather than reusing the generic
      // settings.error string, matching settings.telegram.error's own per-panel pattern.
      loadError: "Couldn't load your shared links. Check your connection and try again.",
      tier: {
        internal: 'Internal',
        public: 'Public',
      },
      expiresIn: 'Expires in {{days}} days',
      expired: 'Expired',
      revoke: 'Revoke',
      // Shared by both revoke paths (per-row and bulk) in both management surfaces — a
      // failed revoke must never fail silently (Rule 2: missing error handling).
      revokeError: 'Could not revoke. Try again.',
      revokeAll: 'Revoke all',
      revokeAllConfirm: {
        title: 'Revoke all shared links?',
        body: 'This removes every active share link. This cannot be undone.',
        confirm: 'Revoke all',
      },
    },
    public: {
      snapshotDate: 'Shared on {{date}}',
      mark: 'Shared from Aura',
      notFound: 'This link is unavailable. It may have expired, been revoked, or never existed.',
      loading: 'Loading the shared conversation…',
      error: 'Something went wrong loading this link. Check your connection and try again.',
      turn: {
        user: 'You',
        assistant: 'Aura',
        toolsUsed: 'Tools used',
      },
      artifacts: {
        heading: 'Artifacts',
        download: 'Download {{name}}',
        previewUnavailable: 'Preview not available for this file — use the download button above.',
      },
    },
  },
};

export const shareIt = {
  share: {
    toggle: {
      label: 'Condividi conversazione',
    },
    modal: {
      title: 'Condividi conversazione',
      tier: {
        legend: 'Chi può aprire questo link',
        internal: {
          label: 'Link interno',
          description: 'Chiunque abbia il link e un account Aura',
        },
        public: {
          label: 'Link pubblico',
          description: 'Chiunque abbia il link, anche senza un account Aura',
        },
      },
      publicWarning:
        'Un link pubblico è visibile a chiunque lo riceva. Revocarlo impedisce nuovi accessi, ma non cancella le copie già viste o salvate nella cache dei motori di ricerca.',
      expiry: {
        legend: 'Scadenza',
        '1d': '1 giorno',
        '7d': '7 giorni',
        '30d': '30 giorni',
        custom: 'Personalizzata',
        customDaysLabel: 'Scadenza personalizzata (giorni)',
      },
      frozenNote:
        'Lo snapshot è congelato ora… i messaggi successivi non compariranno in questo link.',
      cancel: 'Annulla',
      create: 'Crea link',
    },
    shared: {
      urlLabel: 'Link',
      copy: 'Copia',
      copied: 'Copiato ✓',
      meta: '{{tier}} · scade tra {{expiry}} · creato il {{created}}',
      metaNoExpiry: '{{tier}} · creato il {{created}}',
      expiresInDays: '{{count}} giorni',
      stale: '{{count}} nuovi messaggi non sono in questo link',
      update: 'Aggiorna',
      revoke: 'Revoca link',
    },
    revoked: {
      announcement: 'Link revocato',
    },
    revokeConfirm: {
      title: 'Revocare questo link?',
      body: 'Chiunque abbia questo link perderà immediatamente l’accesso. L’azione non può essere annullata.',
      confirm: 'Revoca',
    },
    section: {
      heading: 'Condiviso',
      empty: 'Questa conversazione non è stata condivisa.',
    },
    settings: {
      heading: 'Link condivisi',
      empty: 'Non hai conversazioni condivise.',
      loadError: 'Impossibile caricare i link condivisi. Controlla la connessione e riprova.',
      tier: {
        internal: 'Interno',
        public: 'Pubblico',
      },
      expiresIn: 'Scade tra {{days}} giorni',
      expired: 'Scaduto',
      revoke: 'Revoca',
      revokeError: 'Impossibile revocare. Riprova.',
      revokeAll: 'Revoca tutti',
      revokeAllConfirm: {
        title: 'Revocare tutti i link condivisi?',
        body: 'Questo rimuove tutti i link di condivisione attivi. L’azione non può essere annullata.',
        confirm: 'Revoca tutti',
      },
    },
    public: {
      snapshotDate: 'Condiviso il {{date}}',
      mark: 'Condiviso da Aura',
      notFound:
        'Questo link non è disponibile. Potrebbe essere scaduto, revocato o non essere mai esistito.',
      loading: 'Caricamento della conversazione condivisa…',
      error:
        'Si è verificato un problema nel caricare questo link. Controlla la connessione e riprova.',
      turn: {
        user: 'Tu',
        assistant: 'Aura',
        toolsUsed: 'Strumenti utilizzati',
      },
      artifacts: {
        heading: 'Allegati',
        download: 'Scarica {{name}}',
        previewUnavailable:
          'Anteprima non disponibile per questo file: usa il pulsante di download qui sopra.',
      },
    },
  },
};
