// The `graph.*` i18n feature bundle (Phase 27 Neo4j Graph Explorer) — split out of
// resources.ts to keep that file under the 600-LOC cap (CLAUDE.md "no god class"), the
// resources.display.ts precedent. Every key is from the 27-UI-SPEC §Copywriting Contract.
// resources.ts spreads graphEn/graphIt into each language's `translation` object. Add every
// key to BOTH en AND it — a missing key in either language is a defect.

export const graphEn = {
  graph: {
    title: 'Graph Explorer',
    loading: 'Building the evidence graph…',
    cta: {
      seedConversation: 'Explore this conversation',
      expand: 'Expand neighbors',
    },
    empty: {
      heading: 'No graph for this conversation yet',
      body: "This thread hasn't built any evidence yet. Browse the knowledge graph's structure below, or pick a starting point to explore.",
    },
    inspector: {
      emptyHeading: 'Select a node',
      emptyBody: 'Pick a node on the canvas, or from the list, to see its details, connections, and sources.',
      pinPath: 'Pin path',
      openSource: 'Open source',
      showCypher: 'Show Cypher',
      citations: 'Citations',
      label: 'Type',
      degree: 'Connections',
      neighbors: 'Neighbors',
      properties: 'Properties',
      noCitations: 'No citations for this node.',
    },
    error: {
      query: "Couldn't load the graph. The query failed or the graph service is unavailable. Retry, or check the runtime status.",
      schema: "Couldn't load the graph structure. Retry.",
      auth: 'Your session has expired. Sign in again to view the graph.',
      retry: 'Retry',
    },
    cypher: {
      show: 'Show Cypher',
      hide: 'Hide Cypher',
      note: "Read-only — this is the query that produced this view. You can't edit it.",
    },
    path: {
      label: 'Selected path',
      empty: 'No path selected',
    },
    a11y: {
      nodeList: 'Nodes ({{count}}) — use arrow keys, Enter to inspect',
      edgeList: 'Connections ({{count}})',
      canvasName: 'Evidence graph: {{nodeCount}} nodes, {{edgeCount}} connections',
    },
    filter: {
      labels: 'Node types',
      relTypes: 'Connection types',
    },
    cap: {
      notice: 'Showing the top {{count}} — expand a node to go deeper',
    },
  },
} as const;

export const graphIt = {
  graph: {
    title: 'Esplora grafo',
    loading: 'Costruzione del grafo delle evidenze…',
    cta: {
      seedConversation: 'Esplora questa conversazione',
      expand: 'Espandi vicini',
    },
    empty: {
      heading: 'Ancora nessun grafo per questa conversazione',
      body: 'Questo thread non ha ancora costruito evidenze. Sfoglia la struttura del grafo di conoscenza qui sotto, oppure scegli un punto di partenza da esplorare.',
    },
    inspector: {
      emptyHeading: 'Seleziona un nodo',
      emptyBody: 'Scegli un nodo sulla tela, o dalla lista, per vederne dettagli, connessioni e fonti.',
      pinPath: 'Fissa percorso',
      openSource: 'Apri fonte',
      showCypher: 'Mostra Cypher',
      citations: 'Citazioni',
      label: 'Tipo',
      degree: 'Connessioni',
      neighbors: 'Vicini',
      properties: 'Proprietà',
      noCitations: 'Nessuna citazione per questo nodo.',
    },
    error: {
      query: 'Impossibile caricare il grafo. La query è fallita o il servizio del grafo non è disponibile. Riprova, o controlla lo stato del runtime.',
      schema: 'Impossibile caricare la struttura del grafo. Riprova.',
      auth: 'La tua sessione è scaduta. Accedi di nuovo per visualizzare il grafo.',
      retry: 'Riprova',
    },
    cypher: {
      show: 'Mostra Cypher',
      hide: 'Nascondi Cypher',
      note: 'Sola lettura — questa è la query che ha prodotto questa vista. Non puoi modificarla.',
    },
    path: {
      label: 'Percorso selezionato',
      empty: 'Nessun percorso selezionato',
    },
    a11y: {
      nodeList: 'Nodi ({{count}}) — usa le frecce, Invio per ispezionare',
      edgeList: 'Connessioni ({{count}})',
      canvasName: 'Grafo delle evidenze: {{nodeCount}} nodi, {{edgeCount}} connessioni',
    },
    filter: {
      labels: 'Tipi di nodo',
      relTypes: 'Tipi di connessione',
    },
    cap: {
      notice: 'Mostro i primi {{count}} — espandi un nodo per approfondire',
    },
  },
} as const;
