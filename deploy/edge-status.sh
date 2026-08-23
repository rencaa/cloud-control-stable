#!/usr/bin/env bash
set -euo pipefail

SCRIPT_PATH="$(readlink -f -- "${BASH_SOURCE[0]}")"
PROJECT_ROOT="$(cd -- "$(dirname -- "$SCRIPT_PATH")/.." && pwd)"
COMPOSE=(docker compose --project-name cloud-control-stable -f "$PROJECT_ROOT/docker-compose.edge.yml")
if [[ -f "$PROJECT_ROOT/deploy/tls/fullchain.pem" && -f "$PROJECT_ROOT/deploy/tls/privkey.pem" ]]; then
  COMPOSE+=(-f "$PROJECT_ROOT/docker-compose.tls.yml")
fi

echo "== host memory =="
free -h
echo "== project disk =="
df -h "$PROJECT_ROOT"
echo "== containers =="
"${COMPOSE[@]}" ps
echo "== current container usage =="
docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.PIDs}}'
echo "== persistent data =="
"${COMPOSE[@]}" exec -T server sh -c 'du -sh /app/data /app/backups /app/uploads 2>/dev/null || true'
echo "== health =="
"${COMPOSE[@]}" exec -T server wget -qO- http://127.0.0.1:8080/readyz
echo
