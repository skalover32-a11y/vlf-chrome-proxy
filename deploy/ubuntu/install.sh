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
  mkdir -p "$INSTALL_DIR/deploy/data" "$INSTALL_DIR/deploy/runtime"

  if [ ! -f "$ENV_FILE" ]; then
    log "creating $ENV_FILE from template"
    cp "$INSTALL_DIR/.env.example" "$ENV_FILE"
    log "edit $ENV_FILE and rerun if you need production values"
  fi

  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a

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
      "proxy_port": ${PROXY_PUBLIC_PORT:-1080},
      "proxy_scheme": "socks5",
      "supports_pac": false,
      "status": "online",
      "latency_ms": 0,
      "is_default": true
    }
  ]
}
EOF
  fi
}

start_stack() {
  log "building and starting docker compose stack"
  docker compose -f "$INSTALL_DIR/docker-compose.yml" --env-file "$ENV_FILE" -p vlf_chrome_proxy up -d --build
}

bootstrap_access_link() {
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a

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

main "$@"
