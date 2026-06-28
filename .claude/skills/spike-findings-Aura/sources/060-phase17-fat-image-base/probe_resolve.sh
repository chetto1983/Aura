echo "--- SPEC Req 5 acceptance: all four resolve ---"
for b in python3 node uvx mcp-neo4j-cypher; do
  p=$(command -v "$b" 2>/dev/null || echo MISSING)
  echo "$b -> $p"
done
echo "--- versions ---"
python3 --version
node --version
uv --version
pip show mcp-neo4j-cypher 2>/dev/null | grep -i '^Version'
