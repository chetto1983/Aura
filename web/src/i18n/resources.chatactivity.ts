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

export const chatToolEn = {
  rowAria: '{{tool}} activity',
  expandAria: 'Show {{tool}} details',
  collapseAria: 'Hide {{tool}} details',
  regionAria: '{{tool}} result',
  request: 'Request',
  result: 'Result',
  copy: 'Copy result',
  copied: 'Copied',
  copyAria: 'Copy the raw result',
  meta: {
    results_one: '{{count}} result',
    results_other: '{{count}} results',
    lines_one: '{{count}} line',
    lines_other: '{{count}} lines',
    points_one: '{{count}} point',
    points_other: '{{count}} points',
    workers_one: '{{count}} worker',
    workers_other: '{{count}} workers',
    chars_one: '{{count}} char',
    chars_other: '{{count}} chars',
  },
  duration: {
    seconds: '{{value}} s',
    minutes: '{{minutes}} min {{seconds}} s',
  },
  status: {
    running: 'Running',
    done: 'Done',
    error: 'Error',
  },
};

export const chatToolIt = {
  rowAria: 'Attività di {{tool}}',
  expandAria: 'Mostra i dettagli di {{tool}}',
  collapseAria: 'Nascondi i dettagli di {{tool}}',
  regionAria: 'Risultato di {{tool}}',
  request: 'Richiesta',
  result: 'Risultato',
  copy: 'Copia risultato',
  copied: 'Copiato',
  copyAria: 'Copia il risultato grezzo',
  meta: {
    results_one: '{{count}} risultato',
    results_other: '{{count}} risultati',
    lines_one: '{{count}} riga',
    lines_other: '{{count}} righe',
    points_one: '{{count}} punto',
    points_other: '{{count}} punti',
    workers_one: '{{count}} worker',
    workers_other: '{{count}} worker',
    chars_one: '{{count}} carattere',
    chars_other: '{{count}} caratteri',
  },
  duration: {
    seconds: '{{value}} s',
    minutes: '{{minutes}} min {{seconds}} s',
  },
  status: {
    running: 'In corso',
    done: 'Completato',
    error: 'Errore',
  },
};
