// -- migrate:up
// Legacy adaptive-learning data is irreversibly retired. The predicate names
// only the four verified legacy labels so unrelated graph data survives.
MATCH (n)
WHERE n:ReasoningExample
   OR n:ReasoningSeed
   OR n:ToolSelectionExample
   OR n:ToolSelectionSeed
DETACH DELETE n;
