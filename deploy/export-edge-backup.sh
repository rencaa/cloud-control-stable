#!/usr/bin/env bash
set -euo pipefail

SCRIPT_PATH="$(readlink -f -- "${BASH_SOURCE[0]}")"
PROJECT_ROOT="$(cd -- "$(dirname -- "$SCRIPT_PATH")/.." && pwd)"
EXPORT_ROOT="${1:-/opt/cloud-control-backup-export}"
STAMP="$(date +%Y%m%d-%H%M%S)"
EXPORT_DIR="$EXPORT_ROOT/$STAMP"

mkdir -p "$EXPORT_DIR/sqlite"
chmod 700 "$EXPORT_DIR"

compose=(docker compose --project-name cloud-control-stable -f "$PROJECT_ROOT/docker-compose.edge.yml")
if ! "${compose[@]}" ps --status running server --quiet | grep -q .; then
  echo "Edge server container is not running." >&2
  exit 1
fi
if ! "${compose[@]}" exec -T server sh -c 'ls /app/backups/cloud_control-*.db >/dev/null 2>&1'; then
  echo "No SQLite backup exists yet. Restart the server once, then retry." >&2
  exit 1
fi

"${compose[@]}" cp server:/app/backups/. "$EXPORT_DIR/sqlite"
install -m 600 "$PROJECT_ROOT/.env" "$EXPORT_DIR/runtime.env"

echo "Backup exported to $EXPORT_DIR"
echo "Copy this directory off the 16 GiB host, then verify the copied files."
