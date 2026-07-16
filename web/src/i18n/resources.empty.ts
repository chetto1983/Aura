export const chatEmptyEn = {
  thread: {
    heading: 'Ask Aura',
    body: 'Type a prompt below to start this run.',
    starters: {
      research: {
        label: 'Research a topic',
        body: 'Research [topic] and compare the most reliable sources.',
      },
      file: {
        label: 'Analyze a file',
        body: "Analyze the file I'll attach and summarize the key findings.",
      },
      artifact: {
        label: 'Create an artifact',
        body: 'Create a [report/table/document] about [topic].',
      },
      automation: {
        label: 'Automate a task',
        body: 'Help me plan and automate [repeatable task].',
      },
    },
  },
  suggestionsLabel: 'Suggestions',
} as const;

export const chatEmptyIt = {
  thread: {
    heading: 'Chiedi ad Aura',
    body: 'Scrivi un prompt qui sotto per avviare questa esecuzione.',
    starters: {
      research: {
        label: 'Cerca un argomento',
        body: 'Cerca [argomento] e confronta le fonti più affidabili.',
      },
      file: {
        label: 'Analizza un file',
        body: 'Analizza il file che allegherò e riassumi i risultati principali.',
      },
      artifact: {
        label: 'Crea un artefatto',
        body: 'Crea un [rapporto/tabella/documento] su [argomento].',
      },
      automation: {
        label: "Automatizza un'attività",
        body: 'Aiutami a pianificare e automatizzare [attività ripetitiva].',
      },
    },
  },
  suggestionsLabel: 'Suggerimenti',
} as const;
