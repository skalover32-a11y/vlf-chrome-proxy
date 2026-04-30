# VLF Chrome Proxy Backend MVP

Production-like minimal backend for a Chrome extension that exchanges Telegram-issued access links into short-lived browser sessions and HTTPS proxy credentials.

## What Is Included

- Go backend access/session layer
- HTTPS proxy service with TLS and Basic proxy authentication
- Docker Compose deployment for Ubuntu
- SQLite persistence for MVP
- Admin CLI to create test access links
- Multi-node-ready data model with one-node default deployment

## Architecture

We use one repository and one deployment mode: Docker Compose.

- `api` exposes `/browser/*` endpoints for the Chrome extension.
- `https-proxy` accepts Chrome HTTPS proxy traffic over TLS.
- Proxy auth is validated against backend-issued temporary credentials in SQLite.
- SQLite lives in `deploy/data/app.db`.
- Node metadata lives in `deploy/runtime/nodes.json`.
- TLS material lives in `deploy/runtime/tls/proxy.crt` and `deploy/runtime/tls/proxy.key`.

### Why This HTTPS Proxy Layer

The previous SOCKS5 path is no longer the happy path because Chrome's browser proxy auth behavior is more reliable with HTTP/HTTPS proxy challenges. The MVP now ships a small Go HTTPS proxy daemon in `backend/cmd/https-proxyd`.

It was chosen because it gives us:

- Chrome-compatible `scheme: "https"` fixed proxy config
- TLS between Chrome and the proxy service
- HTTP `CONNECT` tunneling for browser HTTPS traffic
- Basic proxy auth mapped directly to backend-issued temporary credentials
- no external user file reload or sidecar credential sync

The proxy layer is intentionally small: it authenticates, validates destination policy, opens tunnels, and forwards plain HTTP proxy requests.

## Repository Layout

```text
backend/
  cmd/
    api/
    admin/
    https-proxyd/
  internal/
  migrations/
  Dockerfile
  go.mod
deploy/
  ubuntu/install.sh
  data/
  runtime/
configs/
  templates/nodes.json
docs/
  manual-test-plan.md
  security-review.md
docker-compose.yml
.env.example
README.md
```

## Data Model

The access/session model is unchanged by the HTTPS migration.

### `access_links`

- `token_hash`: HMAC hash of the raw access token
- `status`: `active` or revoked/inactive state
- `allowed_node_ids`: JSON array of node IDs allowed for this link
- `default_node_id`: default node for new sessions
- `expires_at`: subscription/access deadline
- `revoked_at`: hard revoke path
- `last_exchanged_at`: audit signal for recent use

### `browser_sessions`

- `session_token_hash`: HMAC hash of the raw browser session token
- `access_link_id`: source access link
- `selected_node_id`: current node choice
- `default_node_id`: node chosen at session creation
- `available_node_ids`: JSON snapshot of nodes exposed to the extension
- `expires_at`: session TTL, default 24h
- `revoked_at`: explicit local logout/revoke path

### `proxy_credentials`

- `session_id`: parent browser session
- `node_id`: target node for which credentials are valid
- `username`: proxy auth username
- `password_version`: version input for deterministic password derivation
- `expires_at`: proxy credential TTL, default 24h
- `revoked_at`: explicit revoke path

### `nodes`

- `id`, `name`, `country`, `city`
- `host`, `proxy_port`, `proxy_scheme`
- `status`, `latency_ms`
- `is_default`

For HTTPS proxy nodes, `proxy_scheme` must be `https`; default public port is `1443`.

## Required API Endpoints

### `POST /browser/exchange-link`

Input:

```json
{
  "url": "https://example.com/access/UNIQUE_TOKEN"
}
```

Behavior:

- parses the full access link
- extracts the token
- validates the token against `access_links`
- creates a short-lived browser session
- creates initial proxy credentials for the default node

### `GET /browser/session`

- validates `Authorization: Bearer <session_token>`
- checks TTL and revoke state
- checks the source access link is still active
- returns current node list

### `GET /browser/proxy-config?node_id=node-1&mode=fixed_servers`

Returns HTTPS proxy config:

```json
{
  "mode": "fixed_servers",
  "host": "proxy.example.com",
  "port": 1443,
  "scheme": "https",
  "username": "browser_u_xxx",
  "password": "browser_p_xxx",
  "bypass_list": ["<local>", "127.0.0.1"]
}
```

## Optional Endpoints In This MVP

- `POST /browser/logout`: implemented and revokes the session plus proxy credentials
- `GET /browser/ip`: stub, returns `501`; the extension treats this as optional
- `GET /browser/pac-config`: stub, returns `501`

## Data Flow

```text
access link URL
  -> POST /browser/exchange-link
  -> browser session created
  -> session_token returned
  -> GET /browser/session
  -> GET /browser/proxy-config?node_id=...
  -> Chrome applies HTTPS fixed proxy
  -> proxy auth challenge
  -> extension supplies temporary username/password
  -> browser traffic tunnels through https-proxyd
```

## TLS Certificates

Chrome connects to the proxy service as an HTTPS proxy, so the proxy endpoint needs a certificate trusted by the client OS/browser.

Runtime paths:

- `deploy/runtime/tls/proxy.crt`
- `deploy/runtime/tls/proxy.key`

Environment paths inside containers:

- `HTTPS_PROXY_TLS_CERT_PATH=/runtime/tls/proxy.crt`
- `HTTPS_PROXY_TLS_KEY_PATH=/runtime/tls/proxy.key`

The installer creates a 30-day self-signed certificate only as a bootstrap fallback. For real Chrome testing, replace it with a trusted certificate for `PROXY_PUBLIC_HOST`.

## Sensitive Fields

Treat these as secrets:

- raw access-link token
- full access-link URL
- `session_token`
- proxy `username`
- proxy `password`
- TLS private key
- `TOKEN_PEPPER`
- `PROXY_PASSWORD_PEPPER`

The backend never logs raw access links or raw session tokens.

## First Local Run

1. Copy the environment template:
   `cp .env.example .env`
2. Edit at least:
   - `TOKEN_PEPPER`
   - `PROXY_PASSWORD_PEPPER`
   - `ACCESS_LINK_BASE_URL`
   - `PROXY_PUBLIC_HOST`
3. Create runtime directories:
   `mkdir -p deploy/data deploy/runtime/tls`
4. Place trusted TLS cert/key into `deploy/runtime/tls/proxy.crt` and `deploy/runtime/tls/proxy.key`, or let the installer create a self-signed smoke-test cert.
5. Copy the node template:
   `cp configs/templates/nodes.json deploy/runtime/nodes.json`
6. Start the stack:
   `docker compose up -d --build`
7. Create a test access link:
   `docker compose --profile tools run --rm admin create-access-link --label local-test --default-node node-1 --expires-in 24h`
8. Use the returned `access_link_url` in the Chrome extension.

## One-Command Ubuntu Deployment

On a clean Ubuntu host:

```bash
curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh | bash
```

What the installer does:

- installs Docker Engine and Docker Compose plugin if missing
- clones or updates the repository to `/opt/vlf-chrome-proxy`
- creates `.env` from `.env.example` if needed
- migrates old SOCKS5 env/node defaults to HTTPS proxy defaults
- creates runtime directories
- generates `deploy/runtime/nodes.json` from env if missing
- creates a self-signed TLS bootstrap cert if no cert/key exists
- builds and starts `api` and `https-proxy`
- removes old compose orphan containers
- optionally creates a bootstrap access link
- prints compose status

## Environment Values You Must Provide

- `ACCESS_LINK_BASE_URL`: public base URL used to form links like `/access/<token>`
- `PROXY_PUBLIC_HOST`: public HTTPS proxy host returned to Chrome
- `PROXY_PUBLIC_PORT`: public HTTPS proxy port, default `1443`
- `BACKEND_PORT`: public backend port, default `18080`
- `HTTPS_PROXY_PORT`: host port mapped to the HTTPS proxy container, default `1443`
- `HTTPS_PROXY_TLS_CERT_PATH`: container path to proxy certificate
- `HTTPS_PROXY_TLS_KEY_PATH`: container path to proxy private key
- `PROXY_ENABLE_IPV6`: enable outbound IPv6 dialing from the proxy, default `false`
- `TOKEN_PEPPER`: HMAC secret for access/session token hashing
- `PROXY_PASSWORD_PEPPER`: HMAC secret for derived proxy passwords

## What You Still Need To Connect

- your real public backend domain
- your real proxy DNS/host
- trusted TLS certificate and key for the proxy host
- the exact access-link format used by your Telegram bot
- the actual issuance path so the bot writes valid `access_links` rows instead of relying on the admin CLI

## Manual Smoke Checks

Check backend:

```bash
curl -fsS http://127.0.0.1:18080/healthz
```

Check HTTPS proxy TLS:

```bash
openssl s_client -connect proxy.example.com:1443 -servername proxy.example.com </dev/null
```

Check proxy auth with issued credentials:

```bash
curl -vk -x https://browser_u_xxx:browser_p_xxx@proxy.example.com:1443 https://api.ipify.org
```

## HTTP Status Model

- `200`: success
- `400`: malformed request or unsupported mode
- `401`: invalid, missing, revoked, or expired session token
- `403`: invalid or expired access link, or link not allowed to use any node
- `404`: node not found
- `409`: node is offline
- `501`: optional endpoint not implemented yet

## Notes Before Production

- Use a trusted TLS certificate for the proxy host before real Chrome testing.
- SQLite is acceptable for one-server MVP, but later multi-node deployments should move session state to Postgres or Redis-backed coordination.
- Restrict allowed Chrome extension IDs before a public rollout.
- Add rate limiting for `/browser/exchange-link` before public traffic.
