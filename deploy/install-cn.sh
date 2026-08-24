#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

MODE="production"
DOMAIN=""
EMAIL=""
PUBLIC_HOST=""
ADMIN_PASSWORD=""
APT_MIRROR="aliyun"
ENABLE_REGISTRATION=0
CREATE_SWAP=1
BACKUP_TARGET=""
BACKUP_SSH_KEY=""
TARGET_DIR="/opt/cloud-control-native"
SERVICE_NAME="cloud-control-native"
NGINX_SITE="/etc/nginx/sites-available/cloud-control-native.conf"
NGINX_LINK="/etc/nginx/sites-enabled/cloud-control-native.conf"
APT_SOURCE_FILE=""
POLICY_CREATED=0

usage() {
  cat <<'EOF'
中国大陆 Ubuntu 24.04/26.04 极速安装器（免 Docker、免服务器编译）

正式 HTTPS：
  sudo bash install-cn.sh --domain control.example.com --email admin@example.com

隔离测试（不占用 80/443）：
  sudo bash install-cn.sh --staging --host 服务器IP

局域网免令牌（手机只填服务器地址）：
  sudo bash install-cn.sh --lan

选项：
  --domain DOMAIN           正式环境域名。
  --email EMAIL             Let's Encrypt 邮箱。
  --staging                 使用 18080 端口运行 HTTP 隔离环境。
  --lan                     使用 18080 端口，仅允许私网地址并关闭设备令牌校验。
  --host HOST               隔离环境的公网 IP 或域名。
  --mirror aliyun|system    默认临时使用阿里云 Ubuntu 镜像，不改系统永久源。
  --admin-password VALUE    指定管理员密码；不提供则自动生成。
  --enable-registration     临时开启设备首次注册。
  --no-swap                 不创建建议的 1 GiB swap。
  --backup-target TARGET    可选：把每日备份同步到本地挂载目录或 user@host:/path。
  --backup-key PATH         远程备份使用的 SSH 私钥绝对路径。
  -h, --help                显示帮助。
EOF
}

log() {
  printf '\n==> %s\n' "$*"
}

die() {
  printf '错误：%s\n' "$*" >&2
  exit 1
}

need_value() {
  [[ $# -ge 2 && -n "${2:-}" ]] || die "$1 缺少参数值"
}

cleanup() {
  if [[ "$POLICY_CREATED" -eq 1 && -f /usr/sbin/policy-rc.d ]]; then
    rm -f /usr/sbin/policy-rc.d
  fi
  if [[ -n "$APT_SOURCE_FILE" && -f "$APT_SOURCE_FILE" ]]; then
    rm -f "$APT_SOURCE_FILE"
  fi
}
trap cleanup EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain)
      need_value "$@"
      DOMAIN="${2,,}"
      MODE="production"
      shift 2
      ;;
    --email)
      need_value "$@"
      EMAIL="$2"
      shift 2
      ;;
    --staging)
      MODE="staging"
      shift
      ;;
    --lan)
      MODE="lan"
      shift
      ;;
    --host)
      need_value "$@"
      PUBLIC_HOST="$2"
      shift 2
      ;;
    --mirror)
      need_value "$@"
      APT_MIRROR="${2,,}"
      shift 2
      ;;
    --admin-password)
      need_value "$@"
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
    --backup-target)
      need_value "$@"
      BACKUP_TARGET="$2"
      shift 2
      ;;
    --backup-key)
      need_value "$@"
      BACKUP_SSH_KEY="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "未知参数：$1"
      ;;
  esac
done

[[ "${EUID:-$(id -u)}" -eq 0 ]] || die "请使用 sudo 运行"
[[ "$APT_MIRROR" == "aliyun" || "$APT_MIRROR" == "system" ]] ||
  die "--mirror 只能是 aliyun 或 system"

[[ -r /etc/os-release ]] || die "无法识别操作系统"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == "ubuntu" && "${VERSION_ID:-}" =~ ^(24\.04|26\.04)$ ]] ||
  die "仅支持 Ubuntu 24.04 或 26.04，当前是 ${PRETTY_NAME:-unknown}"
UBUNTU_CODENAME="${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}"
[[ "$UBUNTU_CODENAME" == "noble" || "$UBUNTU_CODENAME" == "resolute" ]] ||
  die "无法识别 Ubuntu 发布代号，当前是 ${UBUNTU_CODENAME:-unknown}"

ARCHITECTURE="$(dpkg --print-architecture)"
case "$ARCHITECTURE" in
  amd64|arm64) ;;
  *) die "仅支持 amd64 和 arm64，当前是 $ARCHITECTURE" ;;
esac

is_private_ipv4() {
  local ip="$1" a b c d part
  IFS=. read -r a b c d <<<"$ip"
  [[ -n "${a:-}" && -n "${b:-}" && -n "${c:-}" && -n "${d:-}" ]] || return 1
  for part in "$a" "$b" "$c" "$d"; do
    [[ "$part" =~ ^[0-9]+$ && "$part" -le 255 ]] || return 1
  done
  [[ "$a" -eq 10 ]] ||
    [[ "$a" -eq 192 && "$b" -eq 168 ]] ||
    [[ "$a" -eq 172 && "$b" -ge 16 && "$b" -le 31 ]]
}

detect_private_ipv4() {
  local candidate
  while read -r candidate; do
    if is_private_ipv4 "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done < <(hostname -I 2>/dev/null | tr ' ' '\n' | sed '/^$/d')
  return 1
}

if [[ "$MODE" == "production" ]]; then
  [[ "$DOMAIN" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] ||
    die "正式模式必须提供有效的 --domain"
  [[ "$EMAIL" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]] ||
    die "正式模式必须提供有效的 --email"
  PUBLIC_HOST="$DOMAIN"
  PUBLIC_PORT=443
elif [[ "$MODE" == "lan" ]]; then
  if [[ -z "$PUBLIC_HOST" ]]; then
    PUBLIC_HOST="$(detect_private_ipv4 || true)"
  fi
  [[ -n "$PUBLIC_HOST" ]] || die "未检测到 10.x、172.16-31.x 或 192.168.x 局域网地址，请使用 --host 指定"
  is_private_ipv4 "$PUBLIC_HOST" || die "--lan 只接受 RFC1918 局域网 IPv4 地址"
  PUBLIC_PORT=18080
else
  if [[ -z "$PUBLIC_HOST" ]]; then
    PUBLIC_HOST="$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -m1 -E '^[0-9]+(\.[0-9]+){3}$' || true)"
    PUBLIC_HOST="${PUBLIC_HOST:-127.0.0.1}"
  fi
  [[ "$PUBLIC_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] ||
    die "--host 必须是 IPv4 地址或域名"
  PUBLIC_PORT=18080
fi

if [[ "$MODE" != "production" && "$ENABLE_REGISTRATION" -eq 1 ]]; then
  die "HTTP 模式不能开启要求 TLS 的设备自动注册；局域网请直接使用 --lan 免令牌连接"
fi

if [[ -n "$ADMIN_PASSWORD" && ! "$ADMIN_PASSWORD" =~ ^[A-Za-z0-9._~!@%+=:-]{12,128}$ ]]; then
  die "管理员密码必须为 12-128 位，只能使用字母、数字或 ._~!@%+=:-"
fi
if [[ -n "$BACKUP_TARGET" && ! "$BACKUP_TARGET" =~ ^[A-Za-z0-9@._:/-]+$ ]]; then
  die "--backup-target 包含不支持的字符"
fi
if [[ -n "$BACKUP_TARGET" && "$BACKUP_TARGET" != *:* ]]; then
  [[ "$BACKUP_TARGET" == /* ]] || die "本地 --backup-target 必须是绝对路径"
  [[ "$BACKUP_TARGET" != "$TARGET_DIR" && "$BACKUP_TARGET" != "$TARGET_DIR/"* ]] ||
    die "备份同步目标不能位于程序数据目录内"
fi
if [[ -n "$BACKUP_SSH_KEY" ]]; then
  [[ "$BACKUP_SSH_KEY" == /* && -f "$BACKUP_SSH_KEY" ]] || die "--backup-key 必须是已存在的绝对路径"
fi

SCRIPT_PATH="$(readlink -f -- "${BASH_SOURCE[0]}")"
SCRIPT_DIR="$(cd -- "$(dirname -- "$SCRIPT_PATH")" && pwd)"
if [[ -d "$SCRIPT_DIR/bin" ]]; then
  PAYLOAD_ROOT="$SCRIPT_DIR"
  BINARY_DIR="$SCRIPT_DIR/bin"
else
  PAYLOAD_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
  BINARY_DIR="$PAYLOAD_ROOT/server/build"
fi

case "$ARCHITECTURE" in
  amd64) SOURCE_BINARY="$BINARY_DIR/cloud-control-stable-linux-amd64" ;;
  arm64) SOURCE_BINARY="$BINARY_DIR/cloud-control-stable-linux-arm64" ;;
esac
[[ -f "$SOURCE_BINARY" ]] || die "安装包缺少 $ARCHITECTURE 成品程序：$SOURCE_BINARY"

if [[ -f "$PAYLOAD_ROOT/SHA256SUMS" ]]; then
  log "校验离线程序完整性"
  (cd "$PAYLOAD_ROOT" && sha256sum --check --status SHA256SUMS) || die "安装包校验失败，请重新上传"
fi

available_disk_kb="$(df -Pk /opt | awk 'NR==2 {print $4}')"
[[ "$available_disk_kb" -ge 3145728 ]] || die "/opt 至少需要 3 GiB 可用空间"

port_in_use() {
  local port="$1"
  ss -H -ltn | awk '{print $4}' | grep -Eq "(:|\])${port}$"
}

native_service_active() {
  systemctl is-active --quiet "$SERVICE_NAME.service" 2>/dev/null
}

own_nginx_config_active() {
  [[ -L "$NGINX_LINK" && -f "$NGINX_SITE" ]] && systemctl is-active --quiet nginx 2>/dev/null
}

assert_port_free() {
  local port="$1"
  local owner="$2"
  if ! port_in_use "$port"; then
    return
  fi
  case "$owner" in
    backend)
      native_service_active || die "端口 $port 已被其他程序占用，没有停止该程序"
      ;;
    nginx)
      own_nginx_config_active || die "端口 $port 已被其他程序占用，没有停止该程序"
      ;;
  esac
}

if command -v ss >/dev/null 2>&1; then
  assert_port_free 18081 backend
  assert_port_free "$PUBLIC_PORT" nginx
  if [[ "$MODE" == "production" ]]; then
    assert_port_free 80 nginx
  fi
fi

configure_apt() {
  APT_OPTIONS=(
    -o Acquire::Retries=5
    -o Acquire::http::Timeout=20
    -o Acquire::https::Timeout=20
  )
  if [[ "$APT_MIRROR" == "system" ]]; then
    return
  fi

  local mirror_uri="https://mirrors.aliyun.com/ubuntu"
  [[ "$ARCHITECTURE" == "arm64" ]] && mirror_uri="https://mirrors.aliyun.com/ubuntu-ports"
  APT_SOURCE_FILE="/tmp/cloud-control-cn-${$}.sources"
  cat >"$APT_SOURCE_FILE" <<EOF
Types: deb
URIs: $mirror_uri
Suites: $UBUNTU_CODENAME ${UBUNTU_CODENAME}-updates ${UBUNTU_CODENAME}-backports ${UBUNTU_CODENAME}-security
Components: main universe restricted multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
EOF
  APT_OPTIONS+=(
    -o "Dir::Etc::sourcelist=$APT_SOURCE_FILE"
    -o Dir::Etc::sourceparts=-
    -o APT::Get::List-Cleanup=0
  )
}

apt_update_with_fallback() {
  if apt-get "${APT_OPTIONS[@]}" update; then
    return
  fi
  [[ "$APT_MIRROR" == "aliyun" ]] || die "系统软件源更新失败"
  log "阿里云临时镜像不可用，自动回退服务器原有软件源"
  APT_MIRROR="system"
  APT_OPTIONS=(
    -o Acquire::Retries=5
    -o Acquire::http::Timeout=20
    -o Acquire::https::Timeout=20
  )
  apt-get "${APT_OPTIONS[@]}" update
}

configure_swap() {
  [[ "$CREATE_SWAP" -eq 1 ]] || return
  local swap_kb
  swap_kb="$(awk '/SwapTotal:/ {print $2}' /proc/meminfo)"
  [[ "${swap_kb:-0}" -lt 524288 ]] || return
  if [[ -e /swapfile ]]; then
    log "/swapfile 已存在，为避免影响现有系统不修改它"
    return
  fi
  [[ "$(df -Pk / | awk 'NR==2 {print $4}')" -ge 2097152 ]] ||
    die "剩余空间不足，无法创建 1 GiB swap"
  fallocate -l 1G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || printf '/swapfile none swap sw 0 0\n' >>/etc/fstab
  printf 'vm.swappiness=10\n' >/etc/sysctl.d/99-cloud-control.conf
  sysctl -q -p /etc/sysctl.d/99-cloud-control.conf
}

log "使用国内加速源安装 Nginx 和证书组件（不安装 Docker）"
configure_apt

NGINX_INSTALLED_BEFORE=0
dpkg-query -W -f='${Status}' nginx 2>/dev/null | grep -q 'install ok installed' && NGINX_INSTALLED_BEFORE=1
if [[ "$NGINX_INSTALLED_BEFORE" -eq 0 && ! -e /usr/sbin/policy-rc.d ]]; then
  cat >/usr/sbin/policy-rc.d <<'EOF'
#!/bin/sh
exit 101
EOF
  chmod 755 /usr/sbin/policy-rc.d
  POLICY_CREATED=1
fi

export DEBIAN_FRONTEND=noninteractive
apt_update_with_fallback
apt-get "${APT_OPTIONS[@]}" install -y --no-install-recommends \
  ca-certificates curl nginx certbot openssl iproute2 rsync openssh-client sqlite3
if [[ "$POLICY_CREATED" -eq 1 ]]; then
  rm -f /usr/sbin/policy-rc.d
  POLICY_CREATED=0
fi
configure_swap

assert_port_free 18081 backend
assert_port_free "$PUBLIC_PORT" nginx

issue_webroot_certificate() {
  local acme_root="/var/www/cloud-control-acme"
  local acme_site="/etc/nginx/sites-available/cloud-control-native-acme.conf"
  local acme_link="/etc/nginx/sites-enabled/cloud-control-native-acme.conf"
  local previous_link_target=""

  install -d -m 0755 "$acme_root/.well-known/acme-challenge"
  if [[ -L "$NGINX_LINK" ]]; then
    previous_link_target="$(readlink "$NGINX_LINK")"
    rm -f "$NGINX_LINK"
  elif [[ -e "$NGINX_LINK" ]]; then
    die "$NGINX_LINK 不是安装器创建的符号链接，为避免覆盖已停止"
  fi

  cat >"$acme_site" <<EOF
server {
    listen 80;
    server_name $DOMAIN;
    location ^~ /.well-known/acme-challenge/ {
        root $acme_root;
        default_type text/plain;
    }
    location / {
        return 404;
    }
}
EOF
  ln -sfn "$acme_site" "$acme_link"

  if ! nginx -t; then
    rm -f "$acme_link" "$acme_site"
    [[ -z "$previous_link_target" ]] || ln -sfn "$previous_link_target" "$NGINX_LINK"
    die "ACME 临时 Nginx 配置检查失败"
  fi
  systemctl enable nginx >/dev/null
  if systemctl is-active --quiet nginx; then
    systemctl reload nginx
  else
    systemctl start nginx
  fi

  if ! certbot certonly --webroot --webroot-path "$acme_root" \
    --non-interactive --agree-tos --no-eff-email --email "$EMAIL" -d "$DOMAIN"; then
    rm -f "$acme_link" "$acme_site"
    if [[ -n "$previous_link_target" ]]; then
      ln -sfn "$previous_link_target" "$NGINX_LINK"
      nginx -t && systemctl reload nginx || true
    else
      systemctl stop nginx || true
    fi
    die "HTTPS 证书申请失败，请检查域名解析、安全组和 80 端口"
  fi

  rm -f "$acme_link" "$acme_site"
  if [[ -n "$previous_link_target" ]]; then
    ln -sfn "$previous_link_target" "$NGINX_LINK"
    nginx -t && systemctl reload nginx || true
  else
    systemctl stop nginx
  fi
}

if [[ "$MODE" == "production" ]]; then
  if [[ ! -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" || ! -f "/etc/letsencrypt/live/$DOMAIN/privkey.pem" ]]; then
    if port_in_use 80; then
      die "申请证书需要 80 端口，但该端口已被占用；没有停止现有服务"
    fi
    log "申请 HTTPS 证书"
    issue_webroot_certificate
  fi
fi

log "安装预编译云控服务"
getent group cloudcontrol >/dev/null || groupadd --system cloudcontrol
id -u cloudcontrol >/dev/null 2>&1 ||
  useradd --system --gid cloudcontrol --home-dir "$TARGET_DIR" --shell /usr/sbin/nologin cloudcontrol
install -d -m 0750 -o root -g cloudcontrol "$TARGET_DIR"
install -d -m 0750 -o cloudcontrol -g cloudcontrol \
  "$TARGET_DIR/data" "$TARGET_DIR/backups" "$TARGET_DIR/uploads" "$TARGET_DIR/uploads/screenshots"
install -d -m 0755 -o root -g root "$TARGET_DIR/config"

# Keep a complete one-generation software/configuration snapshot. Database
# files are intentionally excluded: schema/data remain forward-only and are
# protected by the verified database backups.
ROLLBACK_DIR="$TARGET_DIR/backups/software-previous"
if [[ -f "$TARGET_DIR/cloud-control-server" ]]; then
  snapshot_new="$TARGET_DIR/backups/.software-previous-new"
  rm -rf -- "$snapshot_new"
  install -d -m 0700 -o root -g root "$snapshot_new"
  snapshot_copy() {
    local source="$1" name="$2"
    if [[ -e "$source" || -L "$source" ]]; then
      cp -a "$source" "$snapshot_new/$name"
    else
      : >"$snapshot_new/$name.missing"
    fi
  }
  snapshot_copy "$TARGET_DIR/cloud-control-server" cloud-control-server
  snapshot_copy "$TARGET_DIR/.env" env
  snapshot_copy "$TARGET_DIR/config/config.json" config-json
  snapshot_copy "$TARGET_DIR/install-credentials.txt" install-credentials
  snapshot_copy "/etc/systemd/system/$SERVICE_NAME.service" service-unit
  snapshot_copy "$NGINX_SITE" nginx-site
  snapshot_copy "$NGINX_LINK" nginx-link
  snapshot_copy /etc/default/cloud-control-native-backup-sync backup-sync-env
  snapshot_copy /usr/local/sbin/cloud-control-native-monitor monitor-script
  snapshot_copy /usr/local/sbin/cloud-control-native-backup-sync backup-sync-script
  snapshot_copy /etc/systemd/system/cloud-control-native-monitor.service monitor-service
  snapshot_copy /etc/systemd/system/cloud-control-native-monitor.timer monitor-timer
  snapshot_copy /etc/systemd/system/cloud-control-native-backup-sync.service backup-sync-service
  snapshot_copy /etc/systemd/system/cloud-control-native-backup-sync.timer backup-sync-timer
  snapshot_copy /usr/local/sbin/cloud-control-native-backup-verify backup-verify-script
  snapshot_copy /etc/systemd/system/cloud-control-native-backup-verify.service backup-verify-service
  snapshot_copy /etc/systemd/system/cloud-control-native-backup-verify.timer backup-verify-timer
  snapshot_copy /etc/logrotate.d/cloud-control-native logrotate
  rm -rf -- "$ROLLBACK_DIR"
  mv "$snapshot_new" "$ROLLBACK_DIR"
fi
if [[ ! -f "$TARGET_DIR/config/config.json" ]]; then
  printf '{}\n' >"$TARGET_DIR/config/config.json"
  chmod 0644 "$TARGET_DIR/config/config.json"
fi

if [[ -f "$TARGET_DIR/cloud-control-server" ]]; then
  cp -a "$TARGET_DIR/cloud-control-server" "$TARGET_DIR/cloud-control-server.previous"
fi
install -m 0755 -o root -g root "$SOURCE_BINARY" "$TARGET_DIR/cloud-control-server.new"
mv -f "$TARGET_DIR/cloud-control-server.new" "$TARGET_DIR/cloud-control-server"

dotenv_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "$TARGET_DIR/.env" 2>/dev/null | tail -n 1
}

if [[ -f "$TARGET_DIR/.env" ]]; then
  cp -a "$TARGET_DIR/.env" "$TARGET_DIR/.env.previous"
fi
JWT_SECRET="$(dotenv_value CLOUD_JWT_SECRET)"
[[ ${#JWT_SECRET} -ge 32 ]] || JWT_SECRET="$(openssl rand -hex 48)"
CURRENT_ADMIN="$(dotenv_value CLOUD_ADMIN_PASSWORD)"
if [[ -n "$ADMIN_PASSWORD" ]]; then
  CURRENT_ADMIN="$ADMIN_PASSWORD"
elif [[ -z "$CURRENT_ADMIN" ]]; then
  CURRENT_ADMIN="$(openssl rand -hex 18)"
fi

if [[ "$MODE" == "production" ]]; then
  CORS_ORIGIN="https://$DOMAIN"
  ACCESS_URL="https://$DOMAIN"
else
  CORS_ORIGIN="http://$PUBLIC_HOST:18080"
  ACCESS_URL="$CORS_ORIGIN"
fi

cat >"$TARGET_DIR/.env.new" <<EOF
CLOUD_INSTALL_MODE=$MODE
CLOUD_MODE=release
CLOUD_DESKTOP_MODE=1
CLOUD_SERVER_PORT=18081
CLOUD_SERVER_BIND_ADDRESS=127.0.0.1
CLOUD_MQTT_PORT=$([[ "$MODE" == "lan" ]] && printf 1883 || printf 0)
CLOUD_MQTT_BIND_ADDRESS=$([[ "$MODE" == "lan" ]] && printf '%s' "$LAN_IP" || printf 127.0.0.1)
CLOUD_DB_DRIVER=sqlite
CLOUD_DB_NAME=$TARGET_DIR/data/cloud_control.db
CLOUD_DB_MAX_OPEN_CONNS=4
CLOUD_DB_MAX_IDLE_CONNS=2
CLOUD_SQLITE_CACHE_KB=8192
CLOUD_SQLITE_MMAP_BYTES=67108864
CLOUD_JWT_SECRET=$JWT_SECRET
CLOUD_ADMIN_PASSWORD=$CURRENT_ADMIN
CLOUD_CORS_ORIGINS=$CORS_ORIGIN
CLOUD_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
CLOUD_DEVICE_AUTH_REQUIRED=$([[ "$MODE" == "lan" ]] && printf false || printf true)
CLOUD_DEVICE_AUTO_REGISTER=$([[ "$MODE" != "lan" && "$ENABLE_REGISTRATION" -eq 1 ]] && printf true || printf false)
CLOUD_AUTO_REGISTER_USER_ID=1
CLOUD_DEVICE_AUTO_REGISTER_RATE_LIMIT=10
CLOUD_DEVICE_AUTO_REGISTER_REQUIRE_TLS=true
CLOUD_DEVICE_WS_ATTEMPTS_PER_MINUTE=$([[ "$MODE" == "lan" ]] && printf 120 || printf 600)
CLOUD_DEVICE_WS_MAX_CONNECTIONS_PER_IP=$([[ "$MODE" == "lan" ]] && printf 8 || printf 256)
CLOUD_RELIABLE_DELIVERY_ENABLED=true
CLOUD_RELIABLE_DELIVERY_RETRY_SECONDS=15
CLOUD_RELIABLE_DELIVERY_MAX_RETRY_SECONDS=300
CLOUD_RELIABLE_DELIVERY_MAX_ATTEMPTS=2048
CLOUD_RELIABLE_DELIVERY_RETRY_BATCH_SIZE=50
CLOUD_RELIABLE_DELIVERY_TTL_HOURS=168
CLOUD_CRON_CATCHUP_HOURS=24
CLOUD_ALERT_DELIVERY_AGE_MINUTES=10
CLOUD_ALERT_QUEUE_USAGE_PERCENT=70
CLOUD_ALERT_CRON_LAG_MINUTES=5
CLOUD_ALERT_RECONNECTS_PER_5_MIN=30
CLOUD_ALERT_COOLDOWN_MINUTES=15
CLOUD_NOTIFY_WEBHOOK_URL=
CLOUD_UPDATE_PUBLIC_KEY=
CLOUD_UPLOAD_PATH=$TARGET_DIR/uploads
CLOUD_UPLOAD_MAX_SIZE=67108864
CLOUD_UPLOAD_MAX_TOTAL_BYTES=2147483648
CLOUD_DEVICE_LOG_RETENTION_DAYS=7
CLOUD_METRIC_RETENTION_DAYS=3
CLOUD_DELIVERY_RETENTION_DAYS=7
CLOUD_TASK_LOG_RETENTION_DAYS=14
CLOUD_SYSTEM_LOG_RETENTION_DAYS=14
CLOUD_SMS_RETENTION_DAYS=30
CLOUD_CLEANUP_BATCH_SIZE=500
CLOUD_SCREENSHOT_RETENTION_HOURS=6
CLOUD_SCREENSHOT_MAX_BYTES=268435456
CLOUD_BACKUP_INTERVAL_HOURS=24
CLOUD_BACKUP_RETENTION_COUNT=7
CLOUD_DISK_WARN_PERCENT=80
CLOUD_DISK_STOP_WRITES_PERCENT=90
CLOUD_TASK_MAX_DEVICES=2000
GOMEMLIMIT=256MiB
GOGC=100
GOMAXPROCS=2
EOF
chown root:cloudcontrol "$TARGET_DIR/.env.new"
chmod 0640 "$TARGET_DIR/.env.new"
mv -f "$TARGET_DIR/.env.new" "$TARGET_DIR/.env"

UNIT_FILE="/etc/systemd/system/$SERVICE_NAME.service"
if [[ -f "$UNIT_FILE" ]]; then
  cp -a "$UNIT_FILE" "$UNIT_FILE.previous"
fi
cat >"$UNIT_FILE" <<EOF
[Unit]
Description=Cloud Control Optimized Native Service v2026.08.24
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60s
StartLimitBurst=10

[Service]
Type=simple
User=cloudcontrol
Group=cloudcontrol
WorkingDirectory=$TARGET_DIR
EnvironmentFile=$TARGET_DIR/.env
ExecStart=$TARGET_DIR/cloud-control-server
Restart=always
RestartSec=3s
TimeoutStopSec=30s
LimitNOFILE=65536
TasksMax=256
MemoryHigh=384M
MemoryMax=512M
CPUQuota=180%
LogRateLimitIntervalSec=30s
LogRateLimitBurst=500
LogsDirectory=cloud-control-native
StandardOutput=append:/var/log/cloud-control-native/server.log
StandardError=append:/var/log/cloud-control-native/server.log
UMask=0027
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectHome=true
ProtectSystem=strict
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=$TARGET_DIR/data $TARGET_DIR/backups $TARGET_DIR/uploads

[Install]
WantedBy=multi-user.target
EOF

rollback_native_service() {
	if [[ -d "$ROLLBACK_DIR" && -f "$ROLLBACK_DIR/cloud-control-server" && -f "$ROLLBACK_DIR/env" ]]; then
		cp -a "$ROLLBACK_DIR/cloud-control-server" "$TARGET_DIR/cloud-control-server"
		cp -a "$ROLLBACK_DIR/env" "$TARGET_DIR/.env"
		[[ ! -f "$ROLLBACK_DIR/config-json" ]] || cp -a "$ROLLBACK_DIR/config-json" "$TARGET_DIR/config/config.json"
		[[ ! -f "$ROLLBACK_DIR/install-credentials" ]] || cp -a "$ROLLBACK_DIR/install-credentials" "$TARGET_DIR/install-credentials.txt"
		[[ ! -f "$ROLLBACK_DIR/service-unit" ]] || cp -a "$ROLLBACK_DIR/service-unit" "$UNIT_FILE"
		[[ ! -f "$ROLLBACK_DIR/nginx-site" ]] || cp -a "$ROLLBACK_DIR/nginx-site" "$NGINX_SITE"
		[[ ! -f "$ROLLBACK_DIR/nginx-link" ]] || cp -a "$ROLLBACK_DIR/nginx-link" "$NGINX_LINK"
		for pair in \
			"backup-sync-env:/etc/default/cloud-control-native-backup-sync" \
			"monitor-script:/usr/local/sbin/cloud-control-native-monitor" \
			"backup-sync-script:/usr/local/sbin/cloud-control-native-backup-sync" \
			"monitor-service:/etc/systemd/system/cloud-control-native-monitor.service" \
			"monitor-timer:/etc/systemd/system/cloud-control-native-monitor.timer" \
			"backup-sync-service:/etc/systemd/system/cloud-control-native-backup-sync.service" \
			"backup-sync-timer:/etc/systemd/system/cloud-control-native-backup-sync.timer" \
			"backup-verify-script:/usr/local/sbin/cloud-control-native-backup-verify" \
			"backup-verify-service:/etc/systemd/system/cloud-control-native-backup-verify.service" \
			"backup-verify-timer:/etc/systemd/system/cloud-control-native-backup-verify.timer" \
			"logrotate:/etc/logrotate.d/cloud-control-native"; do
			name="${pair%%:*}"; target="${pair#*:}"
			if [[ -f "$ROLLBACK_DIR/$name" ]]; then
				cp -a "$ROLLBACK_DIR/$name" "$target"
			elif [[ -f "$ROLLBACK_DIR/$name.missing" ]]; then
				rm -f -- "$target"
			fi
		done
		systemctl daemon-reload
		nginx -t && systemctl reload nginx || true
		systemctl restart "$SERVICE_NAME.service" || true
		return 0
	fi
  if [[ -f "$TARGET_DIR/cloud-control-server.previous" && -f "$TARGET_DIR/.env.previous" ]]; then
    cp -a "$TARGET_DIR/cloud-control-server.previous" "$TARGET_DIR/cloud-control-server"
    cp -a "$TARGET_DIR/.env.previous" "$TARGET_DIR/.env"
    [[ ! -f "$UNIT_FILE.previous" ]] || cp -a "$UNIT_FILE.previous" "$UNIT_FILE"
    systemctl daemon-reload
    systemctl restart "$SERVICE_NAME.service" || true
    return 0
  fi
  return 1
}

cat >/usr/local/sbin/cloud-control-native-rollback <<EOF
#!/usr/bin/env bash
set -Eeuo pipefail
target_dir="$TARGET_DIR"
unit_file="$UNIT_FILE"
service_name="$SERVICE_NAME"
nginx_site="$NGINX_SITE"
nginx_link="$NGINX_LINK"
previous="$ROLLBACK_DIR"
[[ "\${EUID:-\$(id -u)}" -eq 0 ]] || { echo "请使用 sudo 运行" >&2; exit 1; }
if [[ -f "\$previous/cloud-control-server" && -f "\$previous/env" ]]; then
  current="\$target_dir/backups/software-rollback-\$(date +%Y%m%d-%H%M%S)"
  install -d -m 0700 -o root -g root "\$current"
  cp -a "\$target_dir/cloud-control-server" "\$current/cloud-control-server"
  cp -a "\$target_dir/.env" "\$current/.env"
  [[ ! -f "\$unit_file" ]] || cp -a "\$unit_file" "\$current/service-unit"
  [[ ! -f "\$nginx_site" ]] || cp -a "\$nginx_site" "\$current/nginx-site"
  [[ ! -e "\$nginx_link" && ! -L "\$nginx_link" ]] || cp -a "\$nginx_link" "\$current/nginx-link"
  [[ ! -f "\$target_dir/install-credentials.txt" ]] || cp -a "\$target_dir/install-credentials.txt" "\$current/install-credentials"
  restore_item() {
    local name="\$1" target="\$2"
    if [[ -e "\$previous/\$name" || -L "\$previous/\$name" ]]; then
      rm -f -- "\$target"
      cp -a "\$previous/\$name" "\$target"
    elif [[ -f "\$previous/\$name.missing" ]]; then
      rm -f -- "\$target"
    fi
  }
  cp -a "\$previous/cloud-control-server" "\$target_dir/cloud-control-server"
  cp -a "\$previous/env" "\$target_dir/.env"
  restore_item config-json "\$target_dir/config/config.json"
  restore_item install-credentials "\$target_dir/install-credentials.txt"
  restore_item service-unit "\$unit_file"
  restore_item nginx-site "\$nginx_site"
  restore_item nginx-link "\$nginx_link"
  restore_item backup-sync-env /etc/default/cloud-control-native-backup-sync
  restore_item monitor-script /usr/local/sbin/cloud-control-native-monitor
  restore_item backup-sync-script /usr/local/sbin/cloud-control-native-backup-sync
  restore_item monitor-service /etc/systemd/system/cloud-control-native-monitor.service
  restore_item monitor-timer /etc/systemd/system/cloud-control-native-monitor.timer
  restore_item backup-sync-service /etc/systemd/system/cloud-control-native-backup-sync.service
  restore_item backup-sync-timer /etc/systemd/system/cloud-control-native-backup-sync.timer
  restore_item backup-verify-script /usr/local/sbin/cloud-control-native-backup-verify
  restore_item backup-verify-service /etc/systemd/system/cloud-control-native-backup-verify.service
  restore_item backup-verify-timer /etc/systemd/system/cloud-control-native-backup-verify.timer
  restore_item logrotate /etc/logrotate.d/cloud-control-native
  systemctl daemon-reload
  if nginx -t && systemctl reload nginx && systemctl restart "\$service_name.service"; then
    for _ in \$(seq 1 30); do
      if curl --fail --silent http://127.0.0.1:18081/readyz | grep -q '"status":"ready"'; then
        echo "Rollback completed; pre-rollback snapshot: \$current"
        exit 0
      fi
      sleep 2
    done
  fi
  echo "Rollback health check failed; restoring the current version" >&2
  cp -a "\$current/cloud-control-server" "\$target_dir/cloud-control-server"
  cp -a "\$current/.env" "\$target_dir/.env"
  [[ ! -f "\$current/service-unit" ]] || cp -a "\$current/service-unit" "\$unit_file"
  [[ ! -f "\$current/nginx-site" ]] || cp -a "\$current/nginx-site" "\$nginx_site"
  if [[ -e "\$current/nginx-link" || -L "\$current/nginx-link" ]]; then
    rm -f -- "\$nginx_link"
    cp -a "\$current/nginx-link" "\$nginx_link"
  fi
  [[ ! -f "\$current/install-credentials" ]] || cp -a "\$current/install-credentials" "\$target_dir/install-credentials.txt"
  systemctl daemon-reload
  nginx -t && systemctl reload nginx || true
  systemctl restart "\$service_name.service" || true
  exit 1
fi
[[ -f "\$target_dir/cloud-control-server.previous" && -f "\$target_dir/.env.previous" ]] || {
  echo "没有可回滚的上一版本" >&2
  exit 1
}
snapshot="\$target_dir/backups/software-rollback-\$(date +%Y%m%d-%H%M%S)"
install -d -m 0700 -o cloudcontrol -g cloudcontrol "\$snapshot"
cp -a "\$target_dir/cloud-control-server" "\$snapshot/cloud-control-server"
cp -a "\$target_dir/.env" "\$snapshot/.env"
[[ ! -f "\$unit_file" ]] || cp -a "\$unit_file" "\$snapshot/service.unit"
cp -a "\$target_dir/cloud-control-server.previous" "\$target_dir/cloud-control-server"
cp -a "\$target_dir/.env.previous" "\$target_dir/.env"
[[ ! -f "\$unit_file.previous" ]] || cp -a "\$unit_file.previous" "\$unit_file"
systemctl daemon-reload
systemctl restart "\$service_name.service"
for _ in \$(seq 1 30); do
  if curl --fail --silent http://127.0.0.1:18081/readyz | grep -q '"status":"ready"'; then
    echo "已回滚到上一软件版本；回滚前快照：\$snapshot"
    exit 0
  fi
  sleep 2
done
echo "上一版本未通过健康检查，恢复回滚前版本" >&2
cp -a "\$snapshot/cloud-control-server" "\$target_dir/cloud-control-server"
cp -a "\$snapshot/.env" "\$target_dir/.env"
[[ ! -f "\$snapshot/service.unit" ]] || cp -a "\$snapshot/service.unit" "\$unit_file"
systemctl daemon-reload
systemctl restart "\$service_name.service" || true
exit 1
EOF
chmod 0750 /usr/local/sbin/cloud-control-native-rollback

cat >/usr/local/sbin/cloud-control-native-monitor <<EOF
#!/usr/bin/env bash
set -Eeuo pipefail
service_name="$SERVICE_NAME"
target_dir="$TARGET_DIR"
failure_file="/run/cloud-control-native-health-failures"
disk_state_file="/run/cloud-control-native-disk-state"
proxy_ok=0
if [[ "$MODE" == "production" ]]; then
  curl --fail --silent --max-time 5 --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/readyz" | grep -q '"status":"ready"' && proxy_ok=1
else
  curl --fail --silent --max-time 5 http://127.0.0.1:18080/readyz | grep -q '"status":"ready"' && proxy_ok=1
fi
if curl --fail --silent --max-time 5 http://127.0.0.1:18081/readyz | grep -q '"status":"ready"' && [[ "\$proxy_ok" -eq 1 ]]; then
  printf '0\n' >"\$failure_file"
else
  failures=0
  [[ ! -f "\$failure_file" ]] || read -r failures <"\$failure_file"
  failures=\$((failures + 1))
  printf '%s\n' "\$failures" >"\$failure_file"
  logger -t cloud-control-monitor "health check failed (\$failures/3)"
  if [[ "\$failures" -ge 3 ]]; then
    systemctl restart "\$service_name.service"
    systemctl restart nginx
    printf '0\n' >"\$failure_file"
    logger -t cloud-control-monitor "service restarted after repeated health-check failures"
  fi
fi
used_percent="\$(df -P "\$target_dir" | awk 'NR==2 {gsub(/%/, "", \$5); print \$5}')"
disk_state="normal"
if [[ "\${used_percent:-0}" -ge 90 ]]; then
  disk_state="critical"
elif [[ "\${used_percent:-0}" -ge 80 ]]; then
  disk_state="warning"
fi
previous_disk_state=""
[[ ! -f "\$disk_state_file" ]] || read -r previous_disk_state <"\$disk_state_file"
if [[ "\$disk_state" != "\$previous_disk_state" ]]; then
  if [[ "\$disk_state" == "critical" ]]; then
    logger -t cloud-control-monitor "CRITICAL disk usage is \${used_percent}%; large writes are blocked"
  elif [[ "\$disk_state" == "warning" ]]; then
    logger -t cloud-control-monitor "WARNING disk usage is \${used_percent}%"
  elif [[ -n "\$previous_disk_state" ]]; then
    logger -t cloud-control-monitor "disk usage recovered to \${used_percent}%"
  fi
  printf '%s\n' "\$disk_state" >"\$disk_state_file"
fi
EOF
chmod 0755 /usr/local/sbin/cloud-control-native-monitor

cat >/etc/systemd/system/cloud-control-native-monitor.service <<'EOF'
[Unit]
Description=Cloud Control health and disk guard
After=cloud-control-native.service

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/cloud-control-native-monitor
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/run
EOF

cat >/etc/systemd/system/cloud-control-native-monitor.timer <<'EOF'
[Unit]
Description=Run Cloud Control health guard every minute

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
AccuracySec=15s
Persistent=true

[Install]
WantedBy=timers.target
EOF

cat >/usr/local/sbin/cloud-control-native-backup-sync <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
source /etc/default/cloud-control-native-backup-sync
[[ -n "${BACKUP_TARGET:-}" ]] || exit 0
latest="$(find /opt/cloud-control-native/backups -maxdepth 1 -type f -name 'cloud_control-*.db' -printf '%T@ %p\n' | sort -nr | head -n1 | cut -d' ' -f2-)"
[[ -n "$latest" && -f "$latest" ]] || exit 0
checksum="$latest.sha256"
[[ -f "$checksum" ]] || { echo "missing checksum for $latest" >&2; exit 1; }
backup_name="$(basename "$latest")"
required_bytes="$(stat -c '%s' "$latest")"
if [[ "$BACKUP_TARGET" == *:* ]]; then
  ssh_args=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
  [[ -z "${BACKUP_SSH_KEY:-}" ]] || ssh_args+=(-i "$BACKUP_SSH_KEY")
  remote_host="${BACKUP_TARGET%%:*}"
  remote_path="${BACKUP_TARGET#*:}"
  ssh "${ssh_args[@]}" "$remote_host" "mkdir -p '$remote_path'"
  remote_available="$(ssh "${ssh_args[@]}" "$remote_host" "df -PB1 '$remote_path' | awk 'NR==2 {print \$4}'")"
  [[ "${remote_available:-0}" -gt "$required_bytes" ]] || { echo "remote backup target has insufficient free space" >&2; exit 1; }
  rsync -a --partial -e "ssh ${ssh_args[*]}" "$latest" "$checksum" "$BACKUP_TARGET/"
  ssh "${ssh_args[@]}" "$remote_host" "cd '$remote_path' && sha256sum -c '$backup_name.sha256'"
else
  install -d -m 0700 "$BACKUP_TARGET"
  local_available="$(df -PB1 "$BACKUP_TARGET" | awk 'NR==2 {print $4}')"
  [[ "${local_available:-0}" -gt "$required_bytes" ]] || { echo "local backup target has insufficient free space" >&2; exit 1; }
  rsync -a --partial "$latest" "$checksum" "$BACKUP_TARGET/"
  (cd "$BACKUP_TARGET" && sha256sum -c "$backup_name.sha256")
fi
EOF
chmod 0750 /usr/local/sbin/cloud-control-native-backup-sync

cat >/etc/default/cloud-control-native-backup-sync <<EOF
BACKUP_TARGET="$BACKUP_TARGET"
BACKUP_SSH_KEY="$BACKUP_SSH_KEY"
EOF
chmod 0600 /etc/default/cloud-control-native-backup-sync

cat >/etc/systemd/system/cloud-control-native-backup-sync.service <<'EOF'
[Unit]
Description=Sync latest Cloud Control backup
After=cloud-control-native.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/cloud-control-native-backup-sync
Nice=10
IOSchedulingClass=idle
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=read-only
ProtectSystem=full
ReadOnlyPaths=/opt/cloud-control-native/backups
EOF

cat >/etc/systemd/system/cloud-control-native-backup-sync.timer <<'EOF'
[Unit]
Description=Sync Cloud Control backup daily

[Timer]
OnCalendar=*-*-* 03:30:00
RandomizedDelaySec=20min
Persistent=true

[Install]
WantedBy=timers.target
EOF

cat >/usr/local/sbin/cloud-control-native-backup-verify <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
latest="$(find /opt/cloud-control-native/backups -maxdepth 1 -type f -name 'cloud_control-*.db' -printf '%T@ %p\n' | sort -nr | head -n1 | cut -d' ' -f2-)"
[[ -n "$latest" && -f "$latest" ]] || exit 0
(cd "$(dirname "$latest")" && sha256sum -c "$(basename "$latest").sha256")
result="$(sqlite3 -readonly "$latest" 'PRAGMA integrity_check;' | head -n1)"
[[ "$result" == "ok" ]] || { echo "backup restore verification failed: $result" >&2; exit 1; }
logger -t cloud-control-backup "verified backup $(basename "$latest")"
EOF
chmod 0750 /usr/local/sbin/cloud-control-native-backup-verify

cat >/etc/systemd/system/cloud-control-native-backup-verify.service <<'EOF'
[Unit]
Description=Verify latest Cloud Control backup can be restored

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/cloud-control-native-backup-verify
Nice=10
IOSchedulingClass=idle
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadOnlyPaths=/opt/cloud-control-native/backups
EOF

cat >/etc/systemd/system/cloud-control-native-backup-verify.timer <<'EOF'
[Unit]
Description=Verify Cloud Control backup weekly

[Timer]
OnCalendar=Sun *-*-* 04:30:00
RandomizedDelaySec=30min
Persistent=true

[Install]
WantedBy=timers.target
EOF

install -d -m 0750 -o root -g adm /var/log/cloud-control-native
cat >/etc/logrotate.d/cloud-control-native <<'EOF'
/var/log/cloud-control-native/server.log {
    daily
    rotate 7
    size 20M
    missingok
    notifempty
    compress
    delaycompress
    copytruncate
}

/var/log/cloud-control-native/nginx-*.log {
    daily
    rotate 7
    size 20M
    missingok
    notifempty
    compress
    delaycompress
    sharedscripts
    postrotate
        [ ! -s /run/nginx.pid ] || kill -USR1 "$(cat /run/nginx.pid)"
    endscript
}
EOF

systemctl daemon-reload
systemctl enable --now "$SERVICE_NAME.service"

# 局域网模式的手机优先直连 MQTT 1883；UFW 已启用时同步放行。
if [[ "$MODE" == "lan" ]] && command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
  for private_cidr in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16; do
    ufw allow from "$private_cidr" to any port 1883 proto tcp comment 'cloud-control-mqtt-lan' >/dev/null
  done
fi

backend_ready=0
for _ in $(seq 1 45); do
	if curl --fail --silent http://127.0.0.1:18081/readyz | grep -q '"status":"ready"' && \
	  { [[ "$MODE" != "lan" ]] || timeout 2 bash -c "</dev/tcp/$LAN_IP/1883" 2>/dev/null; }; then
    backend_ready=1
    break
  fi
  sleep 2
done
if [[ "$backend_ready" -ne 1 ]]; then
  tail -n 120 /var/log/cloud-control-native/server.log >&2 || true
  if [[ -f "$TARGET_DIR/cloud-control-server.previous" && -f "$TARGET_DIR/.env.previous" ]]; then
    log "新版本启动失败，自动恢复上一个本机版本"
    rollback_native_service || true
  fi
  die "后端健康检查失败"
fi

log "配置 Nginx"
if [[ -f "$NGINX_SITE" ]]; then
  cp -a "$NGINX_SITE" "$NGINX_SITE.previous"
fi
if [[ "$MODE" == "production" ]]; then
  cat >"$NGINX_SITE" <<EOF
upstream cloud_control_native_backend {
    server 127.0.0.1:18081;
    keepalive 32;
}

limit_conn_zone \$binary_remote_addr zone=cloud_device_conn:10m;
limit_req_zone \$binary_remote_addr zone=cloud_device_rate:10m rate=600r/m;

log_format cloud_native_no_args '\$remote_addr - \$remote_user [\$time_local] "\$request_method \$uri \$server_protocol" \$status \$body_bytes_sent "\$http_referer" "\$http_user_agent"';

server {
    listen 80;
    server_name $DOMAIN;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/cloud-control-acme;
        default_type text/plain;
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl http2;
    server_name $DOMAIN;
    server_tokens off;
    client_max_body_size 64m;
    access_log /var/log/cloud-control-native/nginx-access.log cloud_native_no_args;
    error_log /var/log/cloud-control-native/nginx-error.log warn;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:CloudNativeTLS:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;
    add_header Strict-Transport-Security "max-age=31536000" always;

    location = /metrics {
        allow 127.0.0.1;
        allow ::1;
        deny all;
        proxy_pass http://cloud_control_native_backend;
    }

    location /api/ {
        proxy_pass http://cloud_control_native_backend;
        proxy_request_buffering off;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_connect_timeout 5s;
        proxy_read_timeout 120s;
    }

    location /ws/ {
        limit_conn_status 429;
        limit_req_status 429;
        limit_conn cloud_device_conn 64;
        limit_req zone=cloud_device_rate burst=100 nodelay;
        proxy_pass http://cloud_control_native_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 90s;
        proxy_send_timeout 90s;
    }

    location / {
        proxy_pass http://cloud_control_native_backend;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
EOF
else
  cat >"$NGINX_SITE" <<'EOF'
upstream cloud_control_native_backend {
    server 127.0.0.1:18081;
    keepalive 32;
}

limit_conn_zone $binary_remote_addr zone=cloud_device_conn:10m;
limit_req_zone $binary_remote_addr zone=cloud_device_rate:10m rate=60r/m;

log_format cloud_native_no_args '$remote_addr - $remote_user [$time_local] "$request_method $uri $server_protocol" $status $body_bytes_sent "$http_referer" "$http_user_agent"';

server {
    listen 18080;
    server_name _;
    server_tokens off;
    client_max_body_size 64m;
    access_log /var/log/cloud-control-native/nginx-access.log cloud_native_no_args;
    error_log /var/log/cloud-control-native/nginx-error.log warn;

    location = /metrics {
        allow 127.0.0.1;
        allow ::1;
        deny all;
        proxy_pass http://cloud_control_native_backend;
    }

    location /api/ {
        proxy_pass http://cloud_control_native_backend;
        proxy_request_buffering off;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto http;
        proxy_read_timeout 120s;
    }

    location /ws/ {
        limit_conn_status 429;
        limit_req_status 429;
        limit_conn cloud_device_conn 8;
        limit_req zone=cloud_device_rate burst=20 nodelay;
        proxy_pass http://cloud_control_native_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto http;
        proxy_read_timeout 90s;
        proxy_send_timeout 90s;
    }

    location / {
        proxy_pass http://cloud_control_native_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto http;
    }
}
EOF
fi

ln -sfn "$NGINX_SITE" "$NGINX_LINK"
if ! nginx -t; then
  if [[ -f "$NGINX_SITE.previous" ]]; then
    cp -a "$NGINX_SITE.previous" "$NGINX_SITE"
  else
    rm -f "$NGINX_LINK"
    mv -f "$NGINX_SITE" "$NGINX_SITE.failed"
  fi
  rollback_native_service || true
  die "Nginx 配置检查失败，已保留或恢复旧配置和旧服务版本"
fi
systemctl enable nginx >/dev/null
if systemctl is-active --quiet nginx; then
  if ! systemctl reload nginx; then
    [[ ! -f "$NGINX_SITE.previous" ]] || cp -a "$NGINX_SITE.previous" "$NGINX_SITE"
    nginx -t && systemctl reload nginx || true
    rollback_native_service || true
    die "Nginx 重载失败，已尝试恢复旧配置和旧服务版本"
  fi
else
  if ! systemctl start nginx; then
    [[ ! -f "$NGINX_SITE.previous" ]] || cp -a "$NGINX_SITE.previous" "$NGINX_SITE"
    rollback_native_service || true
    die "Nginx 启动失败，已尝试恢复旧配置和旧服务版本"
  fi
fi

if [[ "$MODE" == "production" ]]; then
  install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
  cat >/etc/letsencrypt/renewal-hooks/deploy/cloud-control-native-reload.sh <<'EOF'
#!/bin/sh
systemctl reload nginx
EOF
  chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/cloud-control-native-reload.sh
  systemctl enable --now certbot.timer >/dev/null 2>&1 || true
  if ! curl --fail --silent --show-error --resolve "$DOMAIN:443:127.0.0.1" \
    "https://$DOMAIN/readyz" >/dev/null; then
    rollback_native_service || true
    die "公网入口健康检查失败，已尝试恢复旧服务版本"
  fi
else
  if ! curl --fail --silent --show-error http://127.0.0.1:18080/readyz >/dev/null; then
    rollback_native_service || true
    die "局域网入口健康检查失败，已尝试恢复旧服务版本"
  fi
fi

systemctl enable --now cloud-control-native-monitor.timer >/dev/null
systemctl enable --now cloud-control-native-backup-verify.timer >/dev/null
if [[ -n "$BACKUP_TARGET" ]]; then
  systemctl enable --now cloud-control-native-backup-sync.timer >/dev/null
else
  systemctl disable --now cloud-control-native-backup-sync.timer >/dev/null 2>&1 || true
fi

cat >"$TARGET_DIR/install-credentials.txt" <<EOF
URL=$ACCESS_URL
USERNAME=admin
PASSWORD=$CURRENT_ADMIN
MODE=$MODE
SERVICE=$SERVICE_NAME
CREATED_AT=$(date --iso-8601=seconds)
EOF
chmod 0600 "$TARGET_DIR/install-credentials.txt"

log "安装完成"
printf '访问地址：%s\n' "$ACCESS_URL"
printf '管理员账号：admin\n'
printf '管理员密码：%s\n' "$CURRENT_ADMIN"
printf '凭据文件：%s/install-credentials.txt\n' "$TARGET_DIR"
printf '查看状态：sudo systemctl status %s --no-pager\n' "$SERVICE_NAME"
printf '查看日志：sudo tail -f /var/log/cloud-control-native/server.log\n'
printf '软件回滚：sudo cloud-control-native-rollback\n'
if [[ "$ENABLE_REGISTRATION" -eq 1 ]]; then
  printf '警告：设备自动注册已开启，目标设备注册完成后请立即关闭。\n'
elif [[ "$MODE" == "lan" ]]; then
  printf '局域网免令牌模式已开启：手机只需填写 %s:18080；不要将 18080 暴露到公网。\n' "$PUBLIC_HOST"
else
  printf '设备自动注册保持关闭。\n'
fi
