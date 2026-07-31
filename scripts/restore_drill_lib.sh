#!/usr/bin/env bash

# PostgreSQL folds unquoted identifiers to lower case. Keep the generated database and
# schema identifier byte-identical across SQL (`CREATE DATABASE`) and argv (`pg_restore
# -d`) by normalizing before either consumer sees it.
dr_safe_id() {
  printf '%s' "$1" \
    | tr '[:upper:]' '[:lower:]' \
    | tr -cd 'a-z0-9_' \
    | cut -c1-24
}
