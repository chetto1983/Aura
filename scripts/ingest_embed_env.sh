# Sourced by the ingest E2E scripts that run `python -m ingest.app` directly.

# ingest_container_env <KEY> prints KEY's value in the running aura-ingest container.
ingest_container_env() {
  local entry
  while IFS= read -r entry; do
    case "$entry" in
      "$1="*) printf '%s\n' "${entry#"$1"=}"; return 0 ;;
    esac
  done < <(docker inspect aura-ingest --format '{{range .Config.Env}}{{println .}}{{end}}')
  echo "FAIL: aura-ingest has no $1" >&2
  return 1
}

# ingest_embed_env <image> <network> <out-file> writes the embedding environment a supervised
# child would receive, by asking the supervisor binary itself (-print-embed-env) with the
# running supervisor's own settings DSN and sealing secret. A child a script starts then
# stamps the space the daemon compares against, never one the script made up. The file
# carries the route's credential: keep it in the script's private scratch directory, which
# the script's cleanup removes.
ingest_embed_env() {
  local dsn secret
  dsn="$(ingest_container_env AURA_DB_URL)" || return 1
  secret="$(ingest_container_env AURA_AUTHULA_SECRET)" || return 1
  docker run --rm --network "$2" \
    -e AURA_DB_URL="$dsn" -e AURA_AUTHULA_SECRET="$secret" \
    -e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \
    --entrypoint aura-ingest-supervisor "$1" -print-embed-env > "$3"
}
