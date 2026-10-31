#!/usr/bin/env bash
set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/skalover32-a11y/vlf-chrome-proxy.git}"
INSTALL_DIR="${INSTALL_DIR:-/opt/vlf-chrome-proxy}"
BRANCH="${BRANCH:-main}"
ENV_FILE="${ENV_FILE:-$INSTALL_DIR/.env}"
ENV_WAS_CREATED="false"

log() {
  printf '\n[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

strip_wrapping_quotes() {
  local value="$1"
  if [[ "$value" == \"*\" && "$value" == *\" ]]; then
    value="${value:1:${#value}-2}"
  elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
    value="${value:1:${#value}-2}"
  fi
  printf '%s' "$value"
}

load_env_file() {
  local line key value
  while IFS= read -r line || [ -n "$line" ]; do
    line="$(trim "$line")"
    if [ -z "$line" ] || [[ "$line" == \#* ]]; then
      continue
    fi
    if [[ "$line" != *=* ]]; then
      continue
    fi

    key="$(trim "${line%%=*}")"
    value="${line#*=}"
    value="$(trim "$value")"
    value="$(strip_wrapping_quotes "$value")"

    if [ -n "$key" ]; then
      export "$key=$value"
    fi
  done < "$ENV_FILE"
}

set_env_value() {
  local key="$1"
  local value="$2"

  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

ensure_env_value() {
  local key="$1"
  local value="$2"

  if ! grep -q "^${key}=" "$ENV_FILE"; then
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

ensure_non_empty_env_value() {
  local key="$1"
  local value="$2"
  local current=""

  current="$(grep "^${key}=" "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  if [ -z "$current" ]; then
    set_env_value "$key" "$value"
  fi
}

is_placeholder() {
  local value="${1:-}"
  [ -z "$value" ] ||
    [ "$value" = "replace-with-shared-node-registration-secret" ] ||
    [ "$value" = "proxy.example.com" ] ||
    [ "$value" = "node-1" ] ||
    [ "$value" = "Finland #1" ] ||
    [ "$value" = "FI" ] ||
    [ "$value" = "Helsinki" ]
}

prompt_value() {
  local key="$1"
  local label="$2"
  local fallback="${3:-}"
  local required="${4:-false}"
  local force="${5:-false}"
  local current="${!key:-}"
  local answer=""

  if [ "$force" != "true" ] && [ -n "$current" ] && ! is_placeholder "$current"; then
    return
  fi

  if [ ! -r /dev/tty ]; then
    if [ "$required" = "true" ]; then
      echo "$key is required. Pass it as an env var or run installer from an interactive terminal." >&2
      exit 1
    fi
    return
  fi

  while true; do
    if [ -n "$fallback" ]; then
      printf '%s [%s]: ' "$label" "$fallback" >/dev/tty
    else
      printf '%s: ' "$label" >/dev/tty
    fi
    IFS= read -r answer </dev/tty
    answer="$(trim "$answer")"
    if [ -z "$answer" ]; then
      answer="$fallback"
    fi
    if [ -n "$answer" ] || [ "$required" != "true" ]; then
      set_env_value "$key" "$answer"
      export "$key=$answer"
      return
    fi
  done
}

prompt_role() {
  local current="${NODE_ROLE:-}"
  local answer=""

  if [ -n "$current" ] && [ "$current" != "all_in_one" ]; then
    return
  fi
  if [ "$ENV_WAS_CREATED" != "true" ] && [ -n "$current" ]; then
    return
  fi
  if [ ! -r /dev/tty ]; then
    return
  fi

  while true; do
    printf 'Install role: all_in_one, central, or proxy_node [all_in_one]: ' >/dev/tty
    IFS= read -r answer </dev/tty
    answer="$(trim "$answer")"
    answer="${answer:-all_in_one}"
    case "$answer" in
      all_in_one|central|proxy_node)
        set_env_value "NODE_ROLE" "$answer"
        export NODE_ROLE="$answer"
        return
        ;;
      *)
        echo "Please enter all_in_one, central, or proxy_node." >/dev/tty
        ;;
    esac
  done
}

normalize_proxy_node_identity() {
  local node_id country
  node_id="$(printf '%s' "${NODE_DEFAULT_ID:-}" | tr '[:upper:]' '[:lower:]')"
  country="$(printf '%s' "${NODE_DEFAULT_COUNTRY:-}" | tr '[:lower:]' '[:upper:]')"

  if [[ ! "$node_id" =~ ^[a-z0-9][a-z0-9._-]*$ ]]; then
    echo "NODE_DEFAULT_ID must use lowercase letters, digits, dots, underscores, or hyphens." >&2
    exit 1
  fi
  if [[ ! "$country" =~ ^[A-Z]{2}$ ]]; then
    echo "NODE_DEFAULT_COUNTRY must be a two-letter ISO code, for example FI or DE." >&2
    exit 1
  fi

  set_env_value "NODE_DEFAULT_ID" "$node_id"
  set_env_value "NODE_DEFAULT_COUNTRY" "$country"
  export NODE_DEFAULT_ID="$node_id"
  export NODE_DEFAULT_COUNTRY="$country"
}

configure_install_mode() {
  prompt_role

  case "${NODE_ROLE:-all_in_one}" in
    proxy_node)
      local force_proxy_prompts="false"
      if [ "$ENV_WAS_CREATED" = "true" ]; then
        force_proxy_prompts="true"
      fi
      prompt_value "CENTRAL_BACKEND_URL" "Central backend URL, for example https://api.example.com" "" "true" "$force_proxy_prompts"
      prompt_value "NODE_REGISTRATION_TOKEN" "Node registration token from central backend" "" "true" "$force_proxy_prompts"
      prompt_value "NODE_DEFAULT_ID" "Node id, for example de-1" "$(hostname -s 2>/dev/null || echo node-1)" "true" "$force_proxy_prompts"
      prompt_value "NODE_DEFAULT_NAME" "Node display name, for example Germany #1" "$(hostname -s 2>/dev/null || echo Proxy Node)" "true" "$force_proxy_prompts"
      prompt_value "NODE_DEFAULT_COUNTRY" "Node country code, for example DE" "DE" "true" "$force_proxy_prompts"
      prompt_value "NODE_DEFAULT_CITY" "Node city, for example Frankfurt" "Frankfurt" "true" "$force_proxy_prompts"
      prompt_value "PROXY_PUBLIC_HOST" "Public proxy host for this node, for example de1.example.com" "$(hostname -f 2>/dev/null || hostname -s 2>/dev/null || echo proxy.example.com)" "true" "$force_proxy_prompts"
      prompt_value "PROXY_PUBLIC_PORT" "Public proxy port" "1443" "true" "$force_proxy_prompts"
      normalize_proxy_node_identity
      ;;
    central)
      prompt_value "BACKEND_PORT" "Public backend port" "18080" "true"
      set_env_value "REMNA_NODE_SYNC_ENABLED" "true"
      prompt_value "REMNA_NODE_SYNC_INTERVAL_SECONDS" "Remnawave node sync interval in seconds" "${REMNA_NODE_SYNC_INTERVAL_SECONDS:-300}" "true" "true"
      if is_placeholder "${NODE_REGISTRATION_TOKEN:-}"; then
        ensure_openssl
        set_env_value "NODE_REGISTRATION_TOKEN" "$(openssl rand -hex 32)"
      fi
      ;;
    *)
      prompt_value "PROXY_PUBLIC_HOST" "Public proxy host for this all-in-one server" "$(hostname -f 2>/dev/null || hostname -s 2>/dev/null || echo proxy.example.com)" "true"
      prompt_value "PROXY_PUBLIC_PORT" "Public proxy port" "1443" "true"
      if is_placeholder "${NODE_REGISTRATION_TOKEN:-}"; then
        ensure_openssl
        set_env_value "NODE_REGISTRATION_TOKEN" "$(openssl rand -hex 32)"
      fi
      ;;
  esac
}

install_docker() {
  if need_cmd docker && docker compose version >/dev/null 2>&1; then
    log "docker and docker compose already installed"
    return
  fi

  log "installing docker engine and compose plugin"
  apt-get update -y
  apt-get install -y ca-certificates curl git gnupg lsb-release
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | tee /etc/apt/sources.list.d/docker.list >/dev/null
  apt-get update -y
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin git
  systemctl enable docker
  systemctl start docker
}

docker_login_if_configured() {
  if [ -n "${GHCR_USERNAME:-}" ] && [ -n "${GHCR_TOKEN:-}" ]; then
    log "logging in to ghcr.io"
    printf '%s' "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_USERNAME" --password-stdin
  fi
}

ensure_openssl() {
  if need_cmd openssl; then
    return
  fi

  log "installing openssl for TLS certificate bootstrap"
  apt-get update -y
  apt-get install -y openssl
}

ensure_certbot() {
  if need_cmd certbot; then
    return
  fi

  log "installing certbot for trusted proxy TLS certificate"
  apt-get update -y
  apt-get install -y certbot
}

sync_repo() {
  if [ -d "$INSTALL_DIR/.git" ]; then
    log "updating existing repository in $INSTALL_DIR"
    git -C "$INSTALL_DIR" fetch --all --tags
    git -C "$INSTALL_DIR" checkout "$BRANCH"
    git -C "$INSTALL_DIR" pull --ff-only origin "$BRANCH"
  else
    log "cloning repository into $INSTALL_DIR"
    git clone --branch "$BRANCH" "$REPO_URL" "$INSTALL_DIR"
  fi
}

prepare_runtime() {
  log "preparing runtime directories"
  mkdir -p "$INSTALL_DIR/deploy/data" "$INSTALL_DIR/deploy/runtime" "$INSTALL_DIR/deploy/runtime/tls"

  if [ ! -f "$ENV_FILE" ]; then
    log "creating $ENV_FILE from template"
    cp "$INSTALL_DIR/.env.example" "$ENV_FILE"
    ENV_WAS_CREATED="true"
    log "edit $ENV_FILE and rerun if you need production values"
  fi

  migrate_env_file
  load_env_file
  configure_install_mode
  load_env_file

  if [ "${NODE_ROLE:-all_in_one}" != "all_in_one" ]; then
    log "NODE_ROLE=${NODE_ROLE:-all_in_one}, skipping local nodes.json bootstrap"
  elif [ ! -f "$INSTALL_DIR/deploy/runtime/nodes.json" ]; then
    log "generating initial nodes.json from env"
    cat >"$INSTALL_DIR/deploy/runtime/nodes.json" <<EOF
{
  "nodes": [
    {
      "id": "${NODE_DEFAULT_ID:-node-1}",
      "name": "${NODE_DEFAULT_NAME:-Finland #1}",
      "country": "${NODE_DEFAULT_COUNTRY:-FI}",
      "city": "${NODE_DEFAULT_CITY:-Helsinki}",
      "host": "${PROXY_PUBLIC_HOST:-proxy.example.com}",
      "proxy_port": ${PROXY_PUBLIC_PORT:-1443},
      "proxy_scheme": "https",
      "supports_pac": true,
      "status": "online",
      "latency_ms": 0,
      "is_default": true
    }
  ]
}
EOF
  elif grep -q '"proxy_scheme": "socks5"' "$INSTALL_DIR/deploy/runtime/nodes.json" || grep -q '"proxy_port": 443' "$INSTALL_DIR/deploy/runtime/nodes.json" || grep -q '"supports_pac": false' "$INSTALL_DIR/deploy/runtime/nodes.json"; then
    log "migrating existing nodes.json to HTTPS proxy port ${PROXY_PUBLIC_PORT:-1443}"
    cp "$INSTALL_DIR/deploy/runtime/nodes.json" "$INSTALL_DIR/deploy/runtime/nodes.json.bak.$(date -u +%Y%m%d%H%M%S)"
    cat >"$INSTALL_DIR/deploy/runtime/nodes.json" <<EOF
{
  "nodes": [
    {
      "id": "${NODE_DEFAULT_ID:-node-1}",
      "name": "${NODE_DEFAULT_NAME:-Finland #1}",
      "country": "${NODE_DEFAULT_COUNTRY:-FI}",
      "city": "${NODE_DEFAULT_CITY:-Helsinki}",
      "host": "${PROXY_PUBLIC_HOST:-proxy.example.com}",
      "proxy_port": ${PROXY_PUBLIC_PORT:-1443},
      "proxy_scheme": "https",
      "supports_pac": true,
      "status": "online",
      "latency_ms": 0,
      "is_default": true
    }
  ]
}
EOF
  fi

  if [ "${NODE_ROLE:-all_in_one}" != "central" ]; then
    ensure_tls_material
  fi
}

migrate_env_file() {
  if grep -q '^SOCKS5_' "$ENV_FILE"; then
    log "migrating .env from SOCKS5 defaults to HTTPS proxy defaults"
  fi

  set_env_value "HTTPS_PROXY_LISTEN_ADDR" "${HTTPS_PROXY_LISTEN_ADDR:-:443}"
  set_env_value "HTTPS_PROXY_TLS_CERT_PATH" "${HTTPS_PROXY_TLS_CERT_PATH:-/runtime/tls/proxy.crt}"
  set_env_value "HTTPS_PROXY_TLS_KEY_PATH" "${HTTPS_PROXY_TLS_KEY_PATH:-/runtime/tls/proxy.key}"
  local current_https_port
  current_https_port="$(grep '^HTTPS_PROXY_PORT=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  if [ -z "$current_https_port" ] || [ "$current_https_port" = "443" ]; then
    set_env_value "HTTPS_PROXY_PORT" "1443"
  else
    set_env_value "HTTPS_PROXY_PORT" "$current_https_port"
  fi

  local current_public_port
  current_public_port="$(grep '^PROXY_PUBLIC_PORT=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  if [ -z "$current_public_port" ] || [ "$current_public_port" = "1080" ] || [ "$current_public_port" = "443" ]; then
    set_env_value "PROXY_PUBLIC_PORT" "1443"
  fi

  ensure_env_value "ACCESS_SOURCE_MODE" "local_only"
  ensure_env_value "BUILD_ON_SERVER" "false"
  ensure_env_value "IMAGE_TAG" "main"
  ensure_env_value "NODE_ROLE" "all_in_one"
  ensure_env_value "CENTRAL_BACKEND_URL" ""
  ensure_env_value "NODE_REGISTRATION_TOKEN" "replace-with-shared-node-registration-secret"
  [ -n "${NODE_ROLE:-}" ] && set_env_value "NODE_ROLE" "$NODE_ROLE"
  [ -n "${CENTRAL_BACKEND_URL:-}" ] && set_env_value "CENTRAL_BACKEND_URL" "$CENTRAL_BACKEND_URL"
  [ -n "${NODE_REGISTRATION_TOKEN:-}" ] && set_env_value "NODE_REGISTRATION_TOKEN" "$NODE_REGISTRATION_TOKEN"
  [ -n "${NODE_DEFAULT_ID:-}" ] && set_env_value "NODE_DEFAULT_ID" "$NODE_DEFAULT_ID"
  [ -n "${NODE_DEFAULT_NAME:-}" ] && set_env_value "NODE_DEFAULT_NAME" "$NODE_DEFAULT_NAME"
  [ -n "${NODE_DEFAULT_COUNTRY:-}" ] && set_env_value "NODE_DEFAULT_COUNTRY" "$NODE_DEFAULT_COUNTRY"
  [ -n "${NODE_DEFAULT_CITY:-}" ] && set_env_value "NODE_DEFAULT_CITY" "$NODE_DEFAULT_CITY"
  [ -n "${PROXY_PUBLIC_HOST:-}" ] && set_env_value "PROXY_PUBLIC_HOST" "$PROXY_PUBLIC_HOST"
  [ -n "${PROXY_PUBLIC_PORT:-}" ] && set_env_value "PROXY_PUBLIC_PORT" "$PROXY_PUBLIC_PORT"
  local current_node_role
  current_node_role="$(grep '^NODE_ROLE=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  current_node_role="${NODE_ROLE:-${current_node_role:-all_in_one}}"
  local current_node_token
  current_node_token="$(grep '^NODE_REGISTRATION_TOKEN=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  if [ "$current_node_role" != "proxy_node" ] && { [ -z "$current_node_token" ] || [ "$current_node_token" = "replace-with-shared-node-registration-secret" ]; }; then
    ensure_openssl
    set_env_value "NODE_REGISTRATION_TOKEN" "$(openssl rand -hex 32)"
  fi
  ensure_env_value "REMNA_API_BASE_URL" ""
  ensure_env_value "REMNA_API_TOKEN" ""
  ensure_env_value "REMNA_TIMEOUT_SECONDS" "10"
  ensure_env_value "REMNA_ALLOW_INSECURE_TLS" "false"
  ensure_env_value "REMNA_NODE_SYNC_ENABLED" "false"
  ensure_env_value "REMNA_NODE_SYNC_INTERVAL_SECONDS" "300"
  ensure_env_value "REMNA_NODE_PROXY_PORT" "1443"
  ensure_env_value "REMNA_NODE_SYNC_EXCLUDE_NAMES" "Whitelist"
  ensure_env_value "REMNA_NODE_SYNC_REQUIRE_TAG" "OUT"
  ensure_env_value "REMNA_NODE_SYNC_REQUIRE_PROXY_ALIVE" "true"
  ensure_env_value "REMNA_NODE_SYNC_PROXY_CHECK_TIMEOUT_SECONDS" "3"
  ensure_env_value "REMNA_NODE_SYNC_ALLOW_INSECURE_PROXY_TLS" "false"
  ensure_env_value "ALLOW_SELF_SIGNED_PROXY_CERT" "false"
  ensure_env_value "LETSENCRYPT_EMAIL" ""
  ensure_env_value "LETSENCRYPT_ACCOUNT" ""
  ensure_non_empty_env_value "LETSENCRYPT_CONFIG_DIR" "/etc/letsencrypt-vlf-proxy"
  ensure_non_empty_env_value "LETSENCRYPT_WORK_DIR" "/var/lib/letsencrypt-vlf-proxy"
  ensure_non_empty_env_value "LETSENCRYPT_LOGS_DIR" "/var/log/letsencrypt-vlf-proxy"
  local current_smart_domains
  current_smart_domains="$(grep '^SMART_ROUTING_PROXY_DOMAINS=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  if [ -z "$current_smart_domains" ] || [ "$current_smart_domains" = "2ip.ru,whatismyipaddress.com,youtube.com,googlevideo.com" ]; then
    set_env_value "SMART_ROUTING_PROXY_DOMAINS" ""
  fi
}

ensure_tls_material() {
  local cert_path="$INSTALL_DIR/deploy/runtime/tls/proxy.crt"
  local key_path="$INSTALL_DIR/deploy/runtime/tls/proxy.key"
  local host="${PROXY_PUBLIC_HOST:-proxy.example.com}"

  if [ -f "$cert_path" ] && [ -f "$key_path" ]; then
    ensure_openssl
    if openssl x509 -in "$cert_path" -noout -checkend 86400 >/dev/null 2>&1 && \
      openssl x509 -in "$cert_path" -noout -checkhost "$host" >/dev/null 2>&1; then
      return
    fi
    log "existing proxy TLS certificate is expired, expires soon, or does not match $host; requesting a replacement"
  fi

  if [ "${NODE_ROLE:-all_in_one}" = "proxy_node" ] || [ "${NODE_ROLE:-all_in_one}" = "all_in_one" ]; then
    if obtain_letsencrypt_proxy_cert; then
      return
    fi
  fi

  if [ "${NODE_ROLE:-all_in_one}" = "proxy_node" ] && [ "${ALLOW_SELF_SIGNED_PROXY_CERT:-false}" != "true" ]; then
    echo "Trusted proxy TLS certificate is required for NODE_ROLE=proxy_node." >&2
    echo "Fix DNS/port 80 for PROXY_PUBLIC_HOST=${PROXY_PUBLIC_HOST:-} and rerun the installer." >&2
    echo "Set ALLOW_SELF_SIGNED_PROXY_CERT=true only for local smoke tests; Chrome will reject it for real browsing." >&2
    exit 1
  fi

  ensure_openssl
  log "generating self-signed HTTPS proxy certificate for bootstrap"
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$key_path" \
    -out "$cert_path" \
    -days 30 \
    -subj "/CN=${PROXY_PUBLIC_HOST:-proxy.example.com}" \
    -addext "subjectAltName=DNS:${PROXY_PUBLIC_HOST:-proxy.example.com}" >/dev/null 2>&1
  chmod 600 "$key_path"
  log "replace deploy/runtime/tls/proxy.crt and proxy.key with trusted production certs before real Chrome testing"
}

validate_proxy_tls_files() {
  if [ "${NODE_ROLE:-all_in_one}" = "central" ]; then
    return
  fi

  local cert_path="$INSTALL_DIR/deploy/runtime/tls/proxy.crt"
  local key_path="$INSTALL_DIR/deploy/runtime/tls/proxy.key"

  if [ ! -s "$cert_path" ] || [ ! -s "$key_path" ]; then
    echo "Proxy TLS cert/key are missing." >&2
    echo "Expected: $cert_path and $key_path" >&2
    echo "Fix DNS/port 80 for PROXY_PUBLIC_HOST=${PROXY_PUBLIC_HOST:-} and rerun the installer." >&2
    exit 1
  fi
}

obtain_letsencrypt_proxy_cert() {
  local host="${PROXY_PUBLIC_HOST:-}"
  local cert_path="$INSTALL_DIR/deploy/runtime/tls/proxy.crt"
  local key_path="$INSTALL_DIR/deploy/runtime/tls/proxy.key"

  if [ -z "$host" ] || [ "$host" = "proxy.example.com" ]; then
    log "PROXY_PUBLIC_HOST is not a real domain, skipping Let's Encrypt certificate"
    return 1
  fi
  if [[ "$host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log "PROXY_PUBLIC_HOST is an IP address, skipping Let's Encrypt certificate"
    return 1
  fi
  prepare_certbot_args "$host"
  validate_proxy_dns "$host" || return 1
  if ss -ltn "( sport = :80 )" | tail -n +2 | grep -q .; then
    log "port 80 is already in use, trying nginx webroot certificate flow for $host"
    ss -ltnp "( sport = :80 )" || true
    if obtain_letsencrypt_proxy_cert_with_nginx_webroot "$host" "$cert_path" "$key_path"; then
      return 0
    fi
    log "nginx webroot certificate flow failed for $host"
    return 1
  fi

  ensure_certbot
  log "requesting Let's Encrypt certificate for $host"
  if ! certbot "${certbot_storage_args[@]}" certonly --standalone --non-interactive --agree-tos "${certbot_account_args[@]}" -d "$host"; then
    log "Let's Encrypt certificate request failed for $host, falling back to self-signed certificate"
    return 1
  fi

  copy_letsencrypt_cert "$host" "$cert_path" "$key_path"
  install_renew_hook "$host"
  log "installed trusted Let's Encrypt proxy certificate for $host"
}

certbot_storage_args=()
certbot_account_args=()

prepare_certbot_args() {
  local host="${1:-proxy.example.com}"
  prepare_certbot_storage_args
  prepare_certbot_account_args "$host"
}

prepare_certbot_storage_args() {
  local config_dir="${LETSENCRYPT_CONFIG_DIR:-/etc/letsencrypt-vlf-proxy}"
  local work_dir="${LETSENCRYPT_WORK_DIR:-/var/lib/letsencrypt-vlf-proxy}"
  local logs_dir="${LETSENCRYPT_LOGS_DIR:-/var/log/letsencrypt-vlf-proxy}"

  mkdir -p "$config_dir" "$work_dir" "$logs_dir"
  certbot_storage_args=(--config-dir "$config_dir" --work-dir "$work_dir" --logs-dir "$logs_dir")
}

copy_letsencrypt_cert() {
  local host="$1"
  local cert_path="$2"
  local key_path="$3"
  local config_dir="${LETSENCRYPT_CONFIG_DIR:-/etc/letsencrypt-vlf-proxy}"

  cp "$config_dir/live/$host/fullchain.pem" "$cert_path"
  cp "$config_dir/live/$host/privkey.pem" "$key_path"
  chmod 644 "$cert_path"
  chmod 600 "$key_path"
}

prepare_certbot_account_args() {
  local host="${1:-proxy.example.com}"
  local account="${LETSENCRYPT_ACCOUNT:-}"
  certbot_account_args=()

  if [ -z "$account" ]; then
    account="$(detect_certbot_account_id)"
  fi

  if [ -n "$account" ]; then
    certbot_account_args=(--account "$account")
  elif [ -n "${LETSENCRYPT_EMAIL:-}" ]; then
    certbot_account_args=(--email "$LETSENCRYPT_EMAIL")
  else
    certbot_account_args=(--email "admin@$host")
  fi
}

detect_certbot_account_id() {
  local config_dir="${LETSENCRYPT_CONFIG_DIR:-/etc/letsencrypt-vlf-proxy}"
  local account_dir="$config_dir/accounts/acme-v02.api.letsencrypt.org/directory"
  local first=""

  if [ ! -d "$account_dir" ]; then
    return 0
  fi

  first="$(find "$account_dir" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2>/dev/null | sort | head -n 1 || true)"
  printf '%s' "$first"
}

obtain_letsencrypt_proxy_cert_with_nginx_webroot() {
  local host="$1"
  local cert_path="$2"
  local key_path="$3"
  local webroot="/var/www/vlf-letsencrypt"
  local nginx_conf="/etc/nginx/conf.d/vlf-proxy-acme-${host}.conf"

  if ! need_cmd nginx; then
    log "nginx is not installed, cannot use nginx webroot certificate flow"
    return 1
  fi

  ensure_certbot
  prepare_certbot_args "$host"
  mkdir -p "$webroot/.well-known/acme-challenge"
  chmod 755 "$webroot" "$webroot/.well-known" "$webroot/.well-known/acme-challenge"

  cat >"$nginx_conf" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $host;

    location ^~ /.well-known/acme-challenge/ {
        root $webroot;
        default_type "text/plain";
        try_files \$uri =404;
    }
}
EOF

  if ! nginx -t; then
    log "nginx config test failed after adding $nginx_conf"
    return 1
  fi
  if need_cmd systemctl && systemctl list-unit-files nginx.service >/dev/null 2>&1; then
    systemctl reload nginx
  else
    nginx -s reload
  fi

  log "requesting Let's Encrypt certificate for $host via nginx webroot"
  if ! certbot "${certbot_storage_args[@]}" certonly --webroot -w "$webroot" --non-interactive --agree-tos "${certbot_account_args[@]}" -d "$host"; then
    log "Let's Encrypt webroot certificate request failed for $host"
    return 1
  fi

  copy_letsencrypt_cert "$host" "$cert_path" "$key_path"
  install_renew_hook "$host"
  log "installed trusted Let's Encrypt proxy certificate for $host"
}

install_renew_hook() {
  local host="$1"
  local config_dir="${LETSENCRYPT_CONFIG_DIR:-/etc/letsencrypt-vlf-proxy}"
  local work_dir="${LETSENCRYPT_WORK_DIR:-/var/lib/letsencrypt-vlf-proxy}"
  local logs_dir="${LETSENCRYPT_LOGS_DIR:-/var/log/letsencrypt-vlf-proxy}"
  local hook_path="$config_dir/renewal-hooks/deploy/vlf-proxy-${host}.sh"
  local cron_path="/etc/cron.d/vlf-proxy-certbot-renew"

  mkdir -p "$config_dir/renewal-hooks/deploy"
  cat >"$hook_path" <<EOF
#!/usr/bin/env bash
set -euo pipefail

APP_DIR="$INSTALL_DIR"
DOMAIN="$host"
CONFIG_DIR="$config_dir"

cp "\${CONFIG_DIR}/live/\${DOMAIN}/fullchain.pem" "\${APP_DIR}/deploy/runtime/tls/proxy.crt"
cp "\${CONFIG_DIR}/live/\${DOMAIN}/privkey.pem" "\${APP_DIR}/deploy/runtime/tls/proxy.key"
chmod 644 "\${APP_DIR}/deploy/runtime/tls/proxy.crt"
chmod 600 "\${APP_DIR}/deploy/runtime/tls/proxy.key"

cd "\${APP_DIR}"
docker compose --env-file .env -p vlf_chrome_proxy restart https-proxy
EOF
  chmod +x "$hook_path"

  cat >"$cron_path" <<EOF
17 3 * * * root certbot --config-dir "$config_dir" --work-dir "$work_dir" --logs-dir "$logs_dir" renew --quiet
EOF
}

detect_public_ipv4() {
  local ip=""
  ip="$(curl -4fsS --max-time 8 https://api.ipify.org 2>/dev/null || true)"
  if [ -z "$ip" ]; then
    ip="$(curl -4fsS --max-time 8 https://ifconfig.me 2>/dev/null || true)"
  fi
  printf '%s' "$ip"
}

resolve_ipv4s() {
  local host="$1"
  getent ahostsv4 "$host" | awk '{print $1}' | sort -u | tr '\n' ' '
}

contains_word() {
  local needle="$1"
  local haystack="$2"
  for item in $haystack; do
    if [ "$item" = "$needle" ]; then
      return 0
    fi
  done
  return 1
}

validate_proxy_dns() {
  local host="$1"
  local public_ip
  local resolved_ips

  public_ip="$(detect_public_ipv4)"
  resolved_ips="$(resolve_ipv4s "$host")"

  if [ -z "$resolved_ips" ]; then
    log "DNS check failed: $host does not resolve to any IPv4 address"
    return 1
  fi

  if [ -z "$public_ip" ]; then
    log "public IPv4 detection failed, continuing without DNS/IP precheck"
    return 0
  fi

  if ! contains_word "$public_ip" "$resolved_ips"; then
    log "DNS check failed for $host"
    log "server public IPv4: $public_ip"
    log "DNS IPv4 records: $resolved_ips"
    log "Point $host A record to this server before requesting Let's Encrypt certificate."
    return 1
  fi

  log "DNS check ok: $host resolves to this server IPv4 $public_ip"
  return 0
}

check_port_available() {
  local port="$1"
  local label="$2"

  if ss -ltn "( sport = :$port )" | tail -n +2 | grep -q .; then
    log "$label port $port is already in use"
    ss -ltnp "( sport = :$port )" || true
    echo "Change $label port in $ENV_FILE and rerun the installer." >&2
    return 1
  fi
}

preflight_ports() {
  load_env_file
  case "${NODE_ROLE:-all_in_one}" in
    proxy_node)
      check_port_available "${HTTPS_PROXY_PORT:-1443}" "HTTPS_PROXY_PORT"
      ;;
    central)
      check_port_available "${BACKEND_PORT:-18080}" "BACKEND_PORT"
      ;;
    *)
      check_port_available "${BACKEND_PORT:-18080}" "BACKEND_PORT"
      check_port_available "${HTTPS_PROXY_PORT:-1443}" "HTTPS_PROXY_PORT"
      ;;
  esac
}

start_stack() {
  log "starting docker compose stack"
  docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy down --remove-orphans || true
  preflight_ports
  validate_proxy_tls_files
  docker_login_if_configured

  case "${NODE_ROLE:-all_in_one}" in
    proxy_node)
      if [ "${BUILD_ON_SERVER:-false}" = "true" ]; then
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --build --remove-orphans https-proxy
      else
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy pull https-proxy
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --no-build --remove-orphans https-proxy
      fi
      ;;
    central)
      if [ "${BUILD_ON_SERVER:-false}" = "true" ]; then
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --build --remove-orphans api
      else
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy pull api admin
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --no-build --remove-orphans api
      fi
      ;;
    *)
      if [ "${BUILD_ON_SERVER:-false}" = "true" ]; then
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --build --remove-orphans api https-proxy
      else
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy pull api https-proxy admin
        docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --no-build --remove-orphans api https-proxy
      fi
      ;;
  esac
}

verify_container_running() {
  local service="$1"
  local cid status

  cid="$(docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy ps -q "$service" || true)"
  if [ -z "$cid" ]; then
    echo "Container for service $service was not created." >&2
    docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy ps || true
    exit 1
  fi

  status="$(docker inspect -f '{{.State.Status}}' "$cid" 2>/dev/null || true)"
  if [ "$status" != "running" ]; then
    echo "Container for service $service is not running, current status: ${status:-unknown}" >&2
    docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy logs --tail=120 "$service" || true
    exit 1
  fi
}

verify_backend_health() {
  local port="${BACKEND_PORT:-18080}"

  if ! curl -fsS --max-time 8 "http://127.0.0.1:${port}/healthz" >/dev/null; then
    echo "Backend health check failed on http://127.0.0.1:${port}/healthz" >&2
    docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy logs --tail=120 api || true
    exit 1
  fi
  log "backend health check ok on port $port"
}

verify_proxy_tls() {
  local port="${HTTPS_PROXY_PORT:-1443}"
  local host="${PROXY_PUBLIC_HOST:-localhost}"
  local output_file
  local verified="false"
  local attempt

  ensure_openssl
  output_file="$(mktemp)"
  for attempt in $(seq 1 30); do
    : >"$output_file"
    if [ "${ALLOW_SELF_SIGNED_PROXY_CERT:-false}" = "true" ]; then
      if timeout 8 openssl s_client -connect "127.0.0.1:${port}" -servername "$host" </dev/null >"$output_file" 2>&1; then
        verified="true"
        break
      fi
    elif timeout 8 openssl s_client -connect "127.0.0.1:${port}" -servername "$host" -verify_hostname "$host" -verify_return_error </dev/null >"$output_file" 2>&1; then
      verified="true"
      break
    fi
    sleep 2
  done

  if [ "$verified" != "true" ]; then
    echo "HTTPS proxy TLS verification failed for $host on local port $port." >&2
    echo "Chrome will reject this proxy until PROXY_PUBLIC_HOST has a trusted matching certificate." >&2
    cat "$output_file" >&2 || true
    rm -f "$output_file"
    docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy logs --tail=120 https-proxy || true
    exit 1
  fi
  rm -f "$output_file"
  log "https proxy TLS check ok for $host on port $port"
}

verify_stack() {
  load_env_file
  case "${NODE_ROLE:-all_in_one}" in
    proxy_node)
      verify_container_running "https-proxy"
      verify_proxy_tls
      ;;
    central)
      verify_container_running "api"
      verify_backend_health
      ;;
    *)
      verify_container_running "api"
      verify_container_running "https-proxy"
      verify_backend_health
      verify_proxy_tls
      ;;
  esac
}

bootstrap_access_link() {
  load_env_file

  if [ "${AUTO_CREATE_BOOTSTRAP_LINK:-false}" != "true" ]; then
    log "AUTO_CREATE_BOOTSTRAP_LINK is disabled, skipping bootstrap token creation"
    return
  fi
  if [ "${NODE_ROLE:-all_in_one}" = "proxy_node" ]; then
    log "proxy node role detected, skipping bootstrap token creation"
    return
  fi

  log "creating bootstrap access link if database is empty"
  docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy run --rm admin \
    create-access-link \
    --if-empty \
    --label "${BOOTSTRAP_ACCESS_LINK_LABEL:-bootstrap}" \
    --expires-in "${BOOTSTRAP_ACCESS_LINK_EXPIRES_IN:-720h}" \
    --default-node "${NODE_DEFAULT_ID:-node-1}"
}

show_status() {
  log "docker compose status"
  docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy ps
}

main() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "run this installer as root or with sudo" >&2
    exit 1
  fi

  install_docker
  sync_repo
  prepare_runtime
  start_stack
  verify_stack
  bootstrap_access_link
  show_status
}

if [ "${BASH_SOURCE[0]:-$0}" = "$0" ]; then
  main "$@"
fi
