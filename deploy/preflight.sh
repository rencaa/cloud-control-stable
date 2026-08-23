#!/usr/bin/env bash
set -euo pipefail

EDGE=0
TLS=0
REQUIRE_DOCKER=0
for argument in "$@"; do
  case "$argument" in
    --edge) EDGE=1 ;;
    --tls) TLS=1 ;;
    --require-docker) REQUIRE_DOCKER=1 ;;
    *) echo "Unknown argument: $argument" >&2; exit 2 ;;
  esac
done

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$PROJECT_ROOT/.env"

if [[ "$(basename -- "$PROJECT_ROOT")" != "cloud-control-stable" ]]; then
  echo "Preflight must run from cloud-control-stable." >&2
  exit 1
fi
if [[ ! -f "$ENV_FILE" ]]; then
  echo "Missing .env. Copy .env.example to .env first." >&2
  exit 1
fi

dotenv_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "$ENV_FILE" | tail -n 1
}

required=(CLOUD_JWT_SECRET CLOUD_ADMIN_PASSWORD CLOUD_CORS_ORIGINS)
if [[ "$EDGE" -eq 0 ]]; then
  required+=(MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD MYSQL_ROOT_PASSWORD)
fi
for key in "${required[@]}"; do
  value="$(dotenv_value "$key")"
  if [[ -z "$value" || "$value" == replace-with-* ]]; then
    echo "Set a real value for $key in .env." >&2
    exit 1
  fi
done
jwt_secret="$(dotenv_value CLOUD_JWT_SECRET)"
if [[ "${#jwt_secret}" -lt 32 ]]; then
  echo "CLOUD_JWT_SECRET must contain at least 32 characters." >&2
  exit 1
fi

if [[ "$TLS" -eq 1 ]]; then
  for certificate in fullchain.pem privkey.pem; do
    [[ -f "$PROJECT_ROOT/deploy/tls/$certificate" ]] || {
      echo "Missing deploy/tls/$certificate" >&2
      exit 1
    }
  done
fi

available_kb="$(awk '/MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
available_disk_kb="$(df -Pk "$PROJECT_ROOT" | awk 'NR==2 {print $4}')"
if [[ "$available_kb" -gt 0 && "$available_kb" -lt 524288 ]]; then
  echo "WARNING: less than 512 MiB RAM is currently available. Add swap or stop other services." >&2
fi
if [[ "$available_disk_kb" -lt 4194304 ]]; then
  echo "WARNING: less than 4 GiB disk space is available." >&2
fi

compose_file="docker-compose.yml"
[[ "$EDGE" -eq 1 ]] && compose_file="docker-compose.edge.yml"
compose_args=(-f "$compose_file")
[[ "$TLS" -eq 1 ]] && compose_args+=(-f docker-compose.tls.yml)

if command -v docker >/dev/null 2>&1; then
  (cd "$PROJECT_ROOT" && docker compose --project-name cloud-control-stable "${compose_args[@]}" config --quiet)
elif [[ "$REQUIRE_DOCKER" -eq 1 ]]; then
  echo "Docker is required but unavailable." >&2
  exit 1
else
  echo "WARNING: Docker unavailable; Compose validation skipped." >&2
fi

echo "Preflight passed. Compose file: $compose_file"
