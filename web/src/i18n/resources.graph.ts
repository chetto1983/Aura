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
      heading: 'No evidence graph yet',
      readinessAria: 'Evidence readiness',
      schemaOnline: 'Schema online',
      nodeTypeCount_one: '{{count}} node type',
      nodeTypeCount_other: '{{count}} node types',
      connectionTypeCount_one: '{{count}} connection type',
      connectionTypeCount_other: '{{count}} connection types',
      nodeTypes: 'Node types',
      connectionTypes: 'Connection types',
      body: 'Nothing is linked to this thread yet. The evidence graph fills in as Aura ingests sources and works through conversations — entities, documents, and their connections will appear here.',
    },
    inspector: {
      emptyHeading: 'Select a node',
      emptyBody:
        'Pick a node on the canvas, or from the list, to see its details, connections, and sources.',
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
      query:
        "Couldn't load the graph. The query failed or the graph service is unavailable. Retry, or check the runtime status.",
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
      heading: 'Ancora nessun grafo delle evidenze',
      readinessAria: 'Stato evidenze',
      schemaOnline: 'Schema online',
      nodeTypeCount_one: '{{count}} tipo di nodo',
      nodeTypeCount_other: '{{count}} tipi di nodo',
      connectionTypeCount_one: '{{count}} tipo di connessione',
      connectionTypeCount_other: '{{count}} tipi di connessione',
      nodeTypes: 'Tipi di nodo',
      connectionTypes: 'Tipi di connessione',
      body: 'Niente è ancora collegato a questo thread. Il grafo delle evidenze si popola man mano che Aura acquisisce fonti e lavora sulle conversazioni — entità, documenti e le loro connessioni appariranno qui.',
    },
    inspector: {
      emptyHeading: 'Seleziona un nodo',
      emptyBody:
        'Scegli un nodo sulla tela, o dalla lista, per vederne dettagli, connessioni e fonti.',
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
      query:
        'Impossibile caricare il grafo. La query è fallita o il servizio del grafo non è disponibile. Riprova, o controlla lo stato del runtime.',
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
