// The `chat.skillPicker.*` i18n bundle (Phase 37D composer skill & command picker) — split
// out of resources.ts to keep that file under the 600-LOC cap (CLAUDE.md "no god class"),
// the resources.governance.ts / resources.graph.ts precedent. Unlike the top-level namespace
// bundles (which are spread into `translation`), these are the INNER `skillPicker` object
// referenced as `chat.skillPicker` in each locale — the keys stay nested under `chat`. Add
// every key to BOTH en AND it: a missing key in either language fails the parity gate (D-10).

// The picker's own strings are down to the menu label plus the commands it lists. The
// keys for the pinned-pill, the filter box and the two client-side quick actions went with
// the code that read them: the pill and the hand-rolled listbox were replaced by
// assistant-ui's TriggerPopover (which labels its own filter and needs no pill, the skill
// riding in the message text), and `/new` + `/clear` were removed on the operator's call —
// a menu entry for "start a new chat" beside a sidebar button that already does it, and one
// for "empty the box you are typing in".
export const composerSkillPickerEn = {
  skillsHeader: 'Skills and commands',
  cmdCompact: 'Compact',
  cmdCompactSubtitle: 'Condense the earlier turns into a summary',
};

export const composerSkillPickerIt = {
  skillsHeader: 'Skill e comandi',
  cmdCompact: 'Compatta',
  cmdCompactSubtitle: 'Condensa i turni precedenti in un riassunto',
};

// The `chat.compaction.*` bundle: the in-chat marker the operator reads to see WHERE the
// replayed history stops being verbatim, and the states `/compact` passes through. The
// marker is drawn for every compaction, not only requested ones — the automatic L2.4 pass
// used to leave no trace in the transcript at all.
export const chatCompactionEn = {
  running: 'Compacting the conversation…',
  marker: 'Context compacted',
  markerDetail_one: '{{count}} earlier turn condensed into a summary',
  markerDetail_other: '{{count}} earlier turns condensed into a summary',
  saved: '{{before}} → {{after}} tokens',
  show: 'Show the summary',
  hide: 'Hide the summary',
  kept: 'Every turn is still stored; this is what the model replays.',
  nothing: 'Nothing to compact yet: this conversation has no earlier turns.',
  notWorthwhile:
    'Nothing to gain: a summary of this conversation would be longer than the conversation.',
  unavailable: 'Compaction is not configured for this deployment.',
  failed: 'The summary could not be generated. Nothing in this conversation was changed.',
  dismiss: 'Dismiss',
};

export const chatCompactionIt = {
  running: 'Compattazione della conversazione…',
  marker: 'Contesto compattato',
  markerDetail_one: '{{count}} turno precedente condensato in un riassunto',
  markerDetail_other: '{{count}} turni precedenti condensati in un riassunto',
  saved: '{{before}} → {{after}} token',
  show: 'Mostra il riassunto',
  hide: 'Nascondi il riassunto',
  kept: 'Ogni turno resta memorizzato: questo è ciò che il modello rilegge.',
  nothing: 'Non c’è nulla da compattare: questa conversazione non ha turni precedenti.',
  notWorthwhile:
    'Non c’è nulla da guadagnare: un riassunto di questa conversazione sarebbe più lungo della conversazione.',
  unavailable: 'La compattazione non è configurata in questo deployment.',
  failed: 'Non è stato possibile generare il riassunto. Nella conversazione non è cambiato nulla.',
  dismiss: 'Chiudi',
};

// The `chat.composer.effort.*` bundle (Phase 37E capability-aware reasoning-effort selector). The
// seven level labels map 1:1 onto the UI symbols the /api/composer/reasoning-capabilities endpoint
// advertises (auto·off·low·mid·high·extra·max); the selector renders ONLY the advertised subset
// (D-13), looking each up by symbol as `chat.composer.effort.<symbol>`. `ariaLabel` names the
// control for screen readers. Every key must exist in BOTH locales (the parity gate).
export const composerEffortEn = {
  ariaLabel: 'Reasoning effort',
  auto: 'Auto',
  off: 'Off',
  low: 'Low',
  mid: 'Medium',
  high: 'High',
  extra: 'Extra',
  max: 'Max',
};

export const composerEffortIt = {
  ariaLabel: 'Livello di ragionamento',
  auto: 'Auto',
  off: 'Off',
  low: 'Basso',
  mid: 'Medio',
  high: 'Alto',
  extra: 'Extra',
  max: 'Massimo',
};
