// The `chat.reasoning.*` + `chat.tool.*` compact-chat bundles (spec
// docs/superpowers/specs/2026-07-23-cockpit-compact-chat-ui-spec.md §9) — split
// out of resources.ts per the composerEffort precedent to keep that file under
// the 600-LOC cap. These are INNER objects referenced as `chat.reasoning` /
// `chat.tool` in each locale. Add every key to BOTH en AND it: a missing key in
// either language fails the parity gate (D-10).
//
// The old drawer keys (`reasoning.show/hide`) and raw-chevron keys
// (`tool.showRaw/hideRaw`) are retired with their components; `pending`,
// `duration.*` and `status.*` are carried over unchanged.

export const chatReasoningEn = {
  thinking: 'Thinking…',
  thought: 'Thought for {{duration}}',
  label: 'Reasoning',
  expandAria: 'Show reasoning',
  collapseAria: 'Hide reasoning',
  regionAria: 'Model reasoning',
  pending: 'Thinking...',
};

export const chatReasoningIt = {
  thinking: 'Sto ragionando…',
  thought: 'Ha ragionato per {{duration}}',
  label: 'Ragionamento',
  expandAria: 'Mostra il ragionamento',
  collapseAria: 'Nascondi il ragionamento',
  regionAria: 'Ragionamento del modello',
  pending: 'Ragionamento in corso...',
};
