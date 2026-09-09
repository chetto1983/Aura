package documents

// The document tools are exposed TWICE over the same production handlers: as Aura's own
// agent tools (internal/agent/tools) and as the aura-documents MCP server (cmd/aura). What
// the result means is therefore one contract, and it lives here so the two cannot drift.
//
// They had. Measured 2026-09-09: the MCP's document_search described itself in a single
// line -- "Retrieve full passage evidence, citations and index status" -- naming neither
// citation_token, nor requires_open, nor the neighbours parameter, all three of which the
// agent-side description explained. The consequences were visible in real answers: an agent
// answering from the ArcadeDB manual cited section "6.18.9", which does not exist, while
// the locator it had been handed said "6.4.18. Hybrid Search with vector.fuse"; and it
// listed three of the five option keys because its passage began mid-table and it never
// asked for the adjacent chunk.
//
// Each surface appends only what is true of ITS runtime -- /workspace and shell_exec for
// the agent, nothing of the sort for a bare MCP client -- and nothing else.
const (
	// SearchToolContract explains what a search returns and what to do with it.
	SearchToolContract = "Searches the operator's indexed document library (PDF, DOCX, XLSX, PPTX, CSV, HTML, " +
		"MD, TXT and more) and returns reconciled documents with bounded passages. Each passage carries a " +
		"citation_token, the source SHA-256, and a locator holding the document's OWN heading_path and " +
		"character span; each document carries per-leg retrieval evidence, its size, passage count and index " +
		"time, and the answer carries an explicit degradation or abstention status. " +
		"Read the passages before answering: a filename match alone is not evidence, and abstained:true means " +
		"this library does not hold the answer -- say so rather than answering from your own knowledge. " +
		"Cite ONLY the citation_token and the locator's heading_path returned here. Never cite a section, " +
		"chapter or page number you infer from the passage text: a passage starts wherever its chunk starts, " +
		"so the numbering visible inside it is usually not its own. " +
		"When a passage stops mid-table or mid-definition, the rest of it is in the ADJACENT chunk, which no " +
		"rephrasing of the query will ever rank -- repeat the search with neighbours to pull the passages " +
		"either side of every hit, each with its own citation. " +
		"When a hit reports requires_open, or the question is about the whole file rather than one passage -- " +
		"any count, sum, average, maximum, grouping, sort, cross-column filter or 'how many' over a " +
		"spreadsheet or table, and any conversion -- call document_open with that document_id instead of " +
		"answering from passages. " +
		"query is required and may be a question, a topic, an entity, an exact identifier or a filename; " +
		"document_ids optionally narrows the search to ids already returned to this operator."

	// OpenToolContract explains what opening a document gives back and how to compute on it.
	OpenToolContract = "Writes the ORIGINAL file of an indexed document to disk and returns its path, file " +
		"name, size and the sha256 measured off the written bytes, so it can be read, converted or computed " +
		"on directly. document_search finds WHICH document -- use the document_id from one of its hits; " +
		"document_open hands over the file itself. " +
		"Use it whenever a hit reports requires_open, whenever the answer is a property of the whole file " +
		"rather than of one passage -- any count, sum, average, maximum, grouping, sort, cross-column filter " +
		"or 'how many' -- and whenever the passages document_search returned do not actually " +
		"contain the answer. A spreadsheet especially: it is indexed by description alone and carries no " +
		"passages at all, so chunked text cannot answer an aggregate over one at any relevance while the file " +
		"answers it exactly. " +
		"When a column holds codes rather than quantities -- the card calls them code, and postcodes, ISTAT " +
		"and Belfiore codes, VAT and tax numbers, SKUs and IBANs all are -- load it as TEXT (pandas: " +
		"dtype=str), or the leading zeros that make the value valid are silently dropped and the answer is " +
		"wrong. The card also states which row the column names are on when a banner sits above them."
)
