// The `chat.skillPicker.*` i18n bundle (Phase 37D composer skill & command picker) — split
// out of resources.ts to keep that file under the 600-LOC cap (CLAUDE.md "no god class"),
// the resources.governance.ts / resources.graph.ts precedent. Unlike the top-level namespace
// bundles (which are spread into `translation`), these are the INNER `skillPicker` object
// referenced as `chat.skillPicker` in each locale — the keys stay nested under `chat`. Add
// every key to BOTH en AND it: a missing key in either language fails the parity gate (D-10).

export const composerSkillPickerEn = {
  filterPlaceholder: 'Filter skills and commands',
  commandsHeader: 'Quick commands',
  skillsHeader: 'Skills',
  pinnedRemove: 'Remove pinned skill {{name}}',
  cmdAddFiles: 'Add files',
  cmdAddFilesSubtitle: 'Attach documents to this turn',
  cmdNewChat: 'New chat',
  cmdNewChatSubtitle: 'Start a fresh conversation',
  cmdClear: 'Clear',
  cmdClearSubtitle: 'Reset the composer and pinned skill',
};

export const composerSkillPickerIt = {
  filterPlaceholder: 'Filtra skill e comandi',
  commandsHeader: 'Comandi rapidi',
  skillsHeader: 'Skill',
  pinnedRemove: 'Rimuovi la skill fissata {{name}}',
  cmdAddFiles: 'Aggiungi file',
  cmdAddFilesSubtitle: 'Allega documenti a questo turno',
  cmdNewChat: 'Nuova chat',
  cmdNewChatSubtitle: 'Avvia una nuova conversazione',
  cmdClear: 'Pulisci',
  cmdClearSubtitle: 'Reimposta il composer e la skill fissata',
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
