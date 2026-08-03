# Design QA — identity memory graph

## Visual truth

- Reference: the ArcadeDB Studio graph screenshot supplied by the operator in this conversation (950 × 850). The relevant contract is the graph region: separated circular nodes, labels inside nodes, labelled directed edges, type colour, and a details surface. Aura intentionally retains its own cockpit shell and read-only controls.
- Implementation capture: `D:/tmp/aura-memory-graph-final-chrome.png` (1280 × 720), authenticated as the requested operator and backed by the live identity graph.
- Responsive evidence: the live Playwright graph scenario passed in both `chrome` and `mobile-chrome`; both loaded the same identity-wide graph and expanded a real RID cumulatively.

## Comparison history

1. The initial Sigma capture collapsed the identity graph into one overlapping blob. P0: failed.
2. The first Cytoscape/fCoSE capture separated all 59 nodes but retained timid colour and artificially slow wheel zoom. P1: failed.
3. The final capture uses the Studio renderer/layout family, compact labels inside every node, labelled arrowed edges, cyan `Entity` nodes, amber `FACT` edges, matching type swatches in the filters, and native Cytoscape wheel response. P0/P1/P2: cleared.

## Fidelity checks

| Surface | Result |
| --- | --- |
| Node separation and readable topology | Passed — 59 distinct nodes; no aggregate blob |
| Node names | Passed — centred, wrapped, contrast-adjusted, capped at 45 characters |
| Relationship labels and direction | Passed — outlined autorotated captions and arrowheads |
| Type colour | Passed — deterministic vertex/entity-kind and relationship-type palettes; text remains the primary encoding |
| Inspector and expansion | Passed — existing accessible inspector retained; real RID expansion is cumulative |
| Zoom and resize | Passed — native Cytoscape wheel response, no throttling override; ResizeObserver refits only after container changes |
| Desktop/mobile behavior | Passed — live authenticated scenario green on both configured viewports |
| Bundle impact | Passed — renderer remains lazy; core 435.41 kB and layout 122.56 kB, with no >500 kB warning |

## Final result

passed
