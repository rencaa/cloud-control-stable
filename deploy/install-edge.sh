#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

MODE="staging"
DOMAIN=""
EMAIL=""
PUBLIC_HOST=""
ADMIN_PASSWORD=""
ENABLE_REGISTRATION=0
CREATE_SWAP=1

usage() {
  cat <<'EOF'
Ubuntu 24.04 low-resource one-click installer

Safe staging install (ports 18080/18081, does not occupy 80/443):
  sudo bash deploy/install-edge.sh --staging --host 203.0.113.10

Production HTTPS install:
  sudo bash deploy/install-edge.sh --production \
    --domain control.example.com --email admin@example.com

Options:
  --staging                 Isolated HTTP deployment; this is the default.
  --production              HTTPS deployment on ports 80/443.
  --host HOST               Public IP or hostname used by staging CORS.
  --domain DOMAIN           Public DNS name for production TLS.
  --email EMAIL             Let's Encrypt account email.
  --admin-password VALUE    Set an admin password; otherwise one is generated.
  --enable-registration     Temporarily allow first device registration.
  --no-swap                 Do not create the recommended 1 GiB swap file.
  -h, --help                Show this help.
EOF
}

log() {
  printf '\n==> %s\n' "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_option_value() {
  [[ $# -ge 2 && -n "${2:-}" ]] || die "$1 requires a value"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --staging)
      MODE="staging"
      shift
      ;;
    --production)
      MODE="production"
      shift
      ;;
    --host)
      require_option_value "$@"
      PUBLIC_HOST="$2"
      shift 2
      ;;
    --domain)
      require_option_value "$@"
      DOMAIN="${2,,}"
      shift 2
      ;;
    --email)
      require_option_value "$@"
      EMAIL="$2"
      shift 2
      ;;
    --admin-password)
      require_option_value "$@"
      ADMIN_PASSWORD="$2"
      shift 2
      ;;
    --enable-registration)
      ENABLE_REGISTRATION=1
      shift
      ;;
    --no-swap)
      CREATE_SWAP=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

[[ "${EUID:-$(id -u)}" -eq 0 ]] || die "run this installer with sudo"

SCRIPT_PATH="$(readlink -f -- "${BASH_SOURCE[0]}")"
PROJECT_ROOT="$(cd -- "$(dirname -- "$SCRIPT_PATH")/.." && pwd)"
ENV_FILE="$PROJECT_ROOT/.env"
CREDENTIALS_FILE="$PROJECT_ROOT/install-credentials.txt"

[[ "$(basename -- "$PROJECT_ROOT")" == "cloud-control-stable" ]] ||
  die "the project directory must be named cloud-control-stable"
for required_file in docker-compose.edge.yml docker-compose.tls.yml .env.edge.example server/Dockerfile; do
  [[ -f "$PROJECT_ROOT/$required_file" ]] || die "missing $required_file; upload the complete edge package"
done
chmod +x "$PROJECT_ROOT"/deploy/*.sh

[[ -r /etc/os-release ]] || die "cannot identify the operating system"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == "ubuntu" && "${VERSION_ID:-}" == "24.04" ]] ||
  die "this installer only supports Ubuntu 24.04; detected ${PRETTY_NAME:-unknown}"
case "$(dpkg --print-architecture)" in
  amd64|arm64) ;;
  *) die "only amd64 and arm64 hosts are supported" ;;
esac

if [[ -n "$ADMIN_PASSWORD" && ! "$ADMIN_PASSWORD" =~ ^[A-Za-z0-9._~!@%+=:-]{12,128}$ ]]; then
  die "admin password must be 12-128 characters using letters, numbers, or ._~!@%+=:-"
fi

if [[ "$MODE" == "production" ]]; then
  [[ "$DOMAIN" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] ||
    die "--production requires a valid --domain"
  [[ "$EMAIL" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] ||
    die "--production requires a valid --email"
  PUBLIC_HOST="$DOMAIN"
elif [[ -z "$PUBLIC_HOST" ]]; then
  PUBLIC_HOST="$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -m1 -E '^[0-9]+(\.[0-9]+){3}$' || true)"
  PUBLIC_HOST="${PUBLIC_HOST:-127.0.0.1}"
fi
if [[ "$MODE" == "staging" && ! "$PUBLIC_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]]; then
  die "--host must be an IPv4 address or DNS hostname"
fi

available_disk_kb="$(df -Pk "$PROJECT_ROOT" | awk 'NR==2 {print $4}')"
[[ "$available_disk_kb" -ge 4194304 ]] || die "at least 4 GiB of free disk space is required"

install_docker() {
  if command -v docker >/dev/null 2>&1; then
    docker compose version >/dev/null 2>&1 ||
      die "Docker exists but the Compose plugin is missing; install docker-compose-plugin without replacing active Docker"
    systemctl enable --now docker
    return
  fi

  local conflicting=()
  local package
  for package in docker.io docker-compose docker-compose-v2 podman-docker containerd runc; do
    if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q 'install ok installed'; then
      conflicting+=("$package")
    fi
  done
  if [[ ${#conflicting[@]} -gt 0 ]]; then
    die "conflicting container packages are installed (${conflicting[*]}). They were not removed because that could affect existing services"
  fi

  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  local codename="${UBUNTU_CODENAME:-${VERSION_CODENAME:-noble}}"
  local architecture
  architecture="$(dpkg --print-architecture)"
  cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $codename
Components: stable
Architectures: $architecture
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  systemctl enable --now docker
  docker compose version >/dev/null
}

configure_swap() {
  [[ "$CREATE_SWAP" -eq 1 ]] || return
  local swap_kb
  swap_kb="$(awk '/SwapTotal:/ {print $2}' /proc/meminfo)"
  [[ "${swap_kb:-0}" -lt 524288 ]] || return
  if [[ -e /swapfile ]]; then
    log "/swapfile already exists; leaving it unchanged"
    return
  fi
  [[ "$(df -Pk / | awk 'NR==2 {print $4}')" -ge 2097152 ]] ||
    die "not enough disk space to create the 1 GiB swap file"
  fallocate -l 1G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || printf '/swapfile none swap sw 0 0\n' >>/etc/fstab
  printf 'vm.swappiness=10\n' >/etc/sysctl.d/99-cloud-control.conf
  sysctl -q -p /etc/sysctl.d/99-cloud-control.conf
}

dotenv_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "$ENV_FILE" 2>/dev/null | tail -n 1
}

set_dotenv() {
  local key="$1"
  local value="$2"
  local temporary
  temporary="$(mktemp "$PROJECT_ROOT/.env.install.XXXXXX")"
  awk -v target="$key" -v replacement="$key=$value" '
    BEGIN { found = 0 }
    index($0, target "=") == 1 {
      if (!found) print replacement
      found = 1
      next
    }
    { print }
    END { if (!found) print replacement }
  ' "$ENV_FILE" >"$temporary"
  chmod 600 "$temporary"
  mv -f "$temporary" "$ENV_FILE"
}

port_in_use() {
  local port="$1"
  ss -H -ltn | awk '{print $4}' | grep -Eq "(:|\])${port}$"
}

container_publishes_port() {
  local service="$1"
  local container_port="$2"
  local host_port="$3"
  local container_id
  container_id="$(docker compose --project-name cloud-control-stable -f "$PROJECT_ROOT/docker-compose.edge.yml" ps -q "$service" 2>/dev/null || true)"
  [[ -n "$container_id" ]] || return 1
  docker port "$container_id" "$container_port/tcp" 2>/dev/null | grep -Eq ":${host_port}$"
}

assert_port_available() {
  local host_port="$1"
  local service="$2"
  local container_port="$3"
  if port_in_use "$host_port" && ! container_publishes_port "$service" "$container_port" "$host_port"; then
    die "TCP port $host_port is already occupied by another service; nothing was stopped"
  fi
}

if command -v ss >/dev/null 2>&1; then
  if [[ "$MODE" == "production" ]]; then
    assert_port_available 80 nginx 80
    assert_port_available 443 nginx 443
  else
    assert_port_available 18080 nginx 80
    assert_port_available 18081 server 8080
    assert_port_available 11883 server 1883
  fi
fi

log "Checking host and installing Docker"
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl openssl iproute2
install_docker
configure_swap

if [[ "$MODE" == "production" ]]; then
  assert_port_available 80 nginx 80
  assert_port_available 443 nginx 443
  HTTP_PORT=80
  HTTPS_PORT=443
  CORS_ORIGIN="https://$DOMAIN"
else
  assert_port_available 18080 nginx 80
  assert_port_available 18081 server 8080
  assert_port_available 11883 server 1883
  HTTP_PORT=18080
  HTTPS_PORT=18443
  CORS_ORIGIN="http://$PUBLIC_HOST:$HTTP_PORT"
fi

running_server_id="$(docker compose --project-name cloud-control-stable -f "$PROJECT_ROOT/docker-compose.edge.yml" ps -q server 2>/dev/null || true)"
if [[ -n "$running_server_id" && -f "$ENV_FILE" ]]; then
  existing_http_port="$(dotenv_value HTTP_PORT)"
  if [[ -n "$existing_http_port" && "$existing_http_port" != "$HTTP_PORT" ]]; then
    die "the stable project is already running in another mode on HTTP port $existing_http_port; nothing was changed"
  fi
fi

log "Generating the isolated low-resource configuration"
if [[ ! -f "$ENV_FILE" ]]; then
  install -m 600 "$PROJECT_ROOT/.env.edge.example" "$ENV_FILE"
else
  backup_path="$PROJECT_ROOT/.env.before-install-$(date +%Y%m%d-%H%M%S)"
  install -m 600 "$ENV_FILE" "$backup_path"
  log "Existing .env preserved at $backup_path"
fi

current_jwt="$(dotenv_value CLOUD_JWT_SECRET)"
if [[ -z "$current_jwt" || "$current_jwt" == replace-with-* ]]; then
  current_jwt="$(openssl rand -hex 48)"
fi
current_admin="$(dotenv_value CLOUD_ADMIN_PASSWORD)"
if [[ -n "$ADMIN_PASSWORD" ]]; then
  current_admin="$ADMIN_PASSWORD"
elif [[ -z "$current_admin" || "$current_admin" == replace-with-* ]]; then
  current_admin="$(openssl rand -hex 18)"
fi

set_dotenv CLOUD_JWT_SECRET "$current_jwt"
set_dotenv CLOUD_ADMIN_PASSWORD "$current_admin"
set_dotenv CLOUD_CORS_ORIGINS "$CORS_ORIGIN"
set_dotenv CLOUD_DEVICE_AUTO_REGISTER "$([[ "$ENABLE_REGISTRATION" -eq 1 ]] && printf true || printf false)"
set_dotenv CLOUD_RELIABLE_DELIVERY_ENABLED false
set_dotenv MQTT_BIND_ADDRESS 127.0.0.1
set_dotenv EMBEDDED_MQTT_PORT 11883
set_dotenv SERVER_LOOPBACK_PORT 18081
set_dotenv HTTP_PORT "$HTTP_PORT"
set_dotenv HTTPS_PORT "$HTTPS_PORT"
set_dotenv CLOUD_SQLITE_CACHE_KB 8192
set_dotenv CLOUD_SQLITE_MMAP_BYTES 67108864
chmod 600 "$ENV_FILE"

TLS_ENABLED=0
if [[ "$MODE" == "production" ]]; then
  log "Obtaining and synchronizing the HTTPS certificate"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y certbot
  if [[ ! -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" || ! -f "/etc/letsencrypt/live/$DOMAIN/privkey.pem" ]]; then
    if port_in_use 80 && ! container_publishes_port nginx 80 80; then
      die "port 80 became occupied before certificate issuance"
    fi
    if container_publishes_port nginx 80 80; then
      die "the existing stable Nginx owns port 80 but no renewable certificate exists; stop only that container before retrying"
    fi
    certbot certonly --standalone --non-interactive --agree-tos --no-eff-email \
      --email "$EMAIL" -d "$DOMAIN"
  fi
  "$PROJECT_ROOT/deploy/sync-letsencrypt.sh" "$DOMAIN"
  install -d -m 755 /etc/letsencrypt/renewal-hooks/deploy
  ln -sfn "$PROJECT_ROOT/deploy/sync-letsencrypt.sh" \
    /etc/letsencrypt/renewal-hooks/deploy/cloud-control-sync.sh
  systemctl enable --now certbot.timer >/dev/null 2>&1 || true
  TLS_ENABLED=1
fi

preflight_args=(--edge --require-docker)
compose_args=(
  docker compose --project-name cloud-control-stable
  -f "$PROJECT_ROOT/docker-compose.edge.yml"
)
if [[ "$TLS_ENABLED" -eq 1 ]]; then
  preflight_args+=(--tls)
  compose_args+=(-f "$PROJECT_ROOT/docker-compose.tls.yml")
fi

log "Validating and starting cloud-control-stable"
"$PROJECT_ROOT/deploy/preflight.sh" "${preflight_args[@]}"
"${compose_args[@]}" up -d --build

healthy=0
for _ in $(seq 1 60); do
  if "${compose_args[@]}" exec -T server wget -qO- http://127.0.0.1:8080/readyz 2>/dev/null |
    grep -q '"status":"ready"'; then
    healthy=1
    break
  fi
  sleep 2
done
if [[ "$healthy" -ne 1 ]]; then
  "${compose_args[@]}" ps >&2 || true
  "${compose_args[@]}" logs --tail 200 server nginx >&2 || true
  die "health check failed; containers and volumes were kept for diagnosis"
fi

if [[ "$MODE" == "production" ]]; then
  ACCESS_URL="https://$DOMAIN"
  curl --fail --silent --show-error --resolve "$DOMAIN:443:127.0.0.1" \
    "$ACCESS_URL/readyz" >/dev/null
else
  ACCESS_URL="http://$PUBLIC_HOST:$HTTP_PORT"
  curl --fail --silent --show-error "http://127.0.0.1:18081/readyz" >/dev/null
fi

cat >"$CREDENTIALS_FILE" <<EOF
URL=$ACCESS_URL
USERNAME=admin
PASSWORD=$current_admin
MODE=$MODE
CREATED_AT=$(date --iso-8601=seconds)
EOF
chmod 600 "$CREDENTIALS_FILE"

log "Installation completed"
printf 'Access URL: %s\n' "$ACCESS_URL"
printf 'Admin user: admin\n'
printf 'Admin password: %s\n' "$current_admin"
printf 'Credentials file: %s\n' "$CREDENTIALS_FILE"
printf 'Status command: sudo %s/deploy/edge-status.sh\n' "$PROJECT_ROOT"
printf 'Rollback command (keeps data): sudo docker compose --project-name cloud-control-stable -f %s/docker-compose.edge.yml down\n' "$PROJECT_ROOT"
if [[ "$ENABLE_REGISTRATION" -eq 1 ]]; then
  printf 'WARNING: automatic device registration is enabled. Disable it immediately after the intended devices register.\n'
else
  printf 'Device automatic registration remains disabled for safety.\n'
fi
