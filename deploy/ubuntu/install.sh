#!/usr/bin/env bash
set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/skalover32-a11y/vlf-chrome-proxy.git}"
INSTALL_DIR="${INSTALL_DIR:-/opt/vlf-chrome-proxy}"
BRANCH="${BRANCH:-main}"
ENV_FILE="${ENV_FILE:-$INSTALL_DIR/.env}"

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

ensure_openssl() {
  if need_cmd openssl; then
    return
  fi

  log "installing openssl for TLS certificate bootstrap"
  apt-get update -y
  apt-get install -y openssl
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
    log "edit $ENV_FILE and rerun if you need production values"
  fi

  migrate_env_file
  load_env_file

  if [ ! -f "$INSTALL_DIR/deploy/runtime/nodes.json" ]; then
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
      "proxy_port": ${PROXY_PUBLIC_PORT:-443},
      "proxy_scheme": "https",
      "supports_pac": false,
      "status": "online",
      "latency_ms": 0,
      "is_default": true
    }
  ]
}
EOF
  elif grep -q '"proxy_scheme": "socks5"' "$INSTALL_DIR/deploy/runtime/nodes.json"; then
    log "migrating existing nodes.json from SOCKS5 to HTTPS proxy"
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
      "proxy_port": ${PROXY_PUBLIC_PORT:-443},
      "proxy_scheme": "https",
      "supports_pac": false,
      "status": "online",
      "latency_ms": 0,
      "is_default": true
    }
  ]
}
EOF
  fi

  ensure_tls_material
}

migrate_env_file() {
  if grep -q '^SOCKS5_' "$ENV_FILE"; then
    log "migrating .env from SOCKS5 defaults to HTTPS proxy defaults"
  fi

  set_env_value "HTTPS_PROXY_LISTEN_ADDR" "${HTTPS_PROXY_LISTEN_ADDR:-:443}"
  set_env_value "HTTPS_PROXY_TLS_CERT_PATH" "${HTTPS_PROXY_TLS_CERT_PATH:-/runtime/tls/proxy.crt}"
  set_env_value "HTTPS_PROXY_TLS_KEY_PATH" "${HTTPS_PROXY_TLS_KEY_PATH:-/runtime/tls/proxy.key}"
  set_env_value "HTTPS_PROXY_PORT" "${HTTPS_PROXY_PORT:-443}"

  local current_public_port
  current_public_port="$(grep '^PROXY_PUBLIC_PORT=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- | tr -d '\"' || true)"
  if [ -z "$current_public_port" ] || [ "$current_public_port" = "1080" ]; then
    set_env_value "PROXY_PUBLIC_PORT" "443"
  fi
}

ensure_tls_material() {
  local cert_path="$INSTALL_DIR/deploy/runtime/tls/proxy.crt"
  local key_path="$INSTALL_DIR/deploy/runtime/tls/proxy.key"

  if [ -f "$cert_path" ] && [ -f "$key_path" ]; then
    return
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
  check_port_available "${BACKEND_PORT:-18080}" "BACKEND_PORT"
  check_port_available "${HTTPS_PROXY_PORT:-443}" "HTTPS_PROXY_PORT"
}

start_stack() {
  log "building and starting docker compose stack"
  docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy down --remove-orphans || true
  preflight_ports
  docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --build --remove-orphans
}

bootstrap_access_link() {
  load_env_file

  if [ "${AUTO_CREATE_BOOTSTRAP_LINK:-false}" != "true" ]; then
    log "AUTO_CREATE_BOOTSTRAP_LINK is disabled, skipping bootstrap token creation"
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
  bootstrap_access_link
  show_status
}

if [ "${BASH_SOURCE[0]:-$0}" = "$0" ]; then
  main "$@"
fi
