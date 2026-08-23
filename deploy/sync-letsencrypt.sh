#!/usr/bin/env bash
set -euo pipefail

SCRIPT_PATH="$(readlink -f -- "${BASH_SOURCE[0]}")"
PROJECT_ROOT="$(cd -- "$(dirname -- "$SCRIPT_PATH")/.." && pwd)"
renewed_domains="${RENEWED_DOMAINS:-}"
DOMAIN="${1:-${renewed_domains%% *}}"

if [[ -z "$DOMAIN" ]]; then
  echo "Usage: $0 <certificate-domain>" >&2
  exit 2
fi

SOURCE_DIR="/etc/letsencrypt/live/$DOMAIN"
if [[ ! -f "$SOURCE_DIR/fullchain.pem" || ! -f "$SOURCE_DIR/privkey.pem" ]]; then
  echo "Let's Encrypt certificate not found for $DOMAIN" >&2
  exit 1
fi

install -d -m 700 "$PROJECT_ROOT/deploy/tls"
install -m 644 "$SOURCE_DIR/fullchain.pem" "$PROJECT_ROOT/deploy/tls/fullchain.pem"
install -m 600 "$SOURCE_DIR/privkey.pem" "$PROJECT_ROOT/deploy/tls/privkey.pem"

if command -v docker >/dev/null 2>&1; then
  docker compose \
    --project-name cloud-control-stable \
    -f "$PROJECT_ROOT/docker-compose.edge.yml" \
    -f "$PROJECT_ROOT/docker-compose.tls.yml" \
    exec -T nginx nginx -s reload >/dev/null 2>&1 || true
fi

echo "TLS certificate synchronized for $DOMAIN"
