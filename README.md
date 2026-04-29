# VLF Chrome Proxy Backend MVP

Production-like minimal backend for a Chrome extension that exchanges Telegram-issued access links into short-lived browser sessions and SOCKS5 proxy credentials.

## What is included

- Go backend access/session layer
- Real SOCKS5 proxy service using a dedicated Go daemon
- Docker Compose deployment for Ubuntu
- SQLite persistence for MVP
- Admin CLI to create test access links
- Multi-node-ready data model with one-node default deployment

## Architecture

We use one repository and one deployment mode: Docker Compose.

- `api` service exposes `/browser/*` endpoints for the Chrome extension
- `socks5` service validates username/password auth against shared backend state
- SQLite lives in `deploy/data/app.db`
- Node metadata lives in `deploy/runtime/nodes.json`

### Why this SOCKS5 server

This MVP ships with a dedicated Go SOCKS5 daemon built from the same codebase and backed by [`github.com/things-go/go-socks5`](https://pkg.go.dev/github.com/things-go/go-socks5). I picked this path because it gives us:

- stable SOCKS5 protocol handling
- built-in username/password auth support
- direct runtime validation against our session and credential store
- a single deploy contour without external proxy-specific config drift

For the first version this is a better fit than bolting dynamic credentials onto an external daemon with file reload tricks.

## Repository layout

```text
backend/
  cmd/
    api/
    admin/
    socks5d/
  internal/
  migrations/
  Dockerfile
  go.mod
deploy/
  ubuntu/install.sh
configs/
  templates/nodes.json
docs/
  manual-test-plan.md
  security-review.md
docker-compose.yml
.env.example
README.md
```

## Data model

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

## Required API endpoints

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

- validates the browser session
- validates the requested node
- lazily creates node-specific proxy credentials if needed
- returns SOCKS5 config for the extension

## Optional endpoints in this MVP

- `POST /browser/logout`: implemented and revokes the session plus proxy credentials
- `GET /browser/ip`: stub, returns `501`
- `GET /browser/pac-config`: stub, returns `501`

## Access-link flow

```text
access link URL
  -> POST /browser/exchange-link
  -> browser session created
  -> session_token returned
  -> GET /browser/session
  -> GET /browser/proxy-config?node_id=...
  -> SOCKS5 username/password auth against socks5d
```

## Sensitive fields

Treat these as secrets:

- raw access-link token
- full access-link URL
- `session_token`
- proxy `username`
- proxy `password`
- `TOKEN_PEPPER`
- `PROXY_PASSWORD_PEPPER`

The backend never logs raw access links or raw session tokens.

## First local run

1. Copy the environment template:
   `cp .env.example .env`
2. Edit at least:
   - `TOKEN_PEPPER`
   - `PROXY_PASSWORD_PEPPER`
   - `ACCESS_LINK_BASE_URL`
   - `PROXY_PUBLIC_HOST`
3. Create runtime directories:
   `mkdir -p deploy/data deploy/runtime`
4. Copy the node template:
   `cp configs/templates/nodes.json deploy/runtime/nodes.json`
5. Start the stack:
   `docker compose up -d --build`
6. Create a test access link:
   `docker compose --profile tools run --rm admin create-access-link --label local-test --default-node node-1 --expires-in 24h`
7. Use the returned `access_link_url` in the Chrome extension.

## One-command Ubuntu deployment

On a clean Ubuntu host:

```bash
curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh | sudo bash
```

What the installer does:

- installs Docker Engine and Docker Compose plugin if missing
- clones or updates the repository to `/opt/vlf-chrome-proxy`
- creates `.env` from `.env.example` if needed
- creates runtime directories
- generates `deploy/runtime/nodes.json` from env if missing
- builds and starts `api` and `socks5`
- optionally creates a bootstrap access link
- prints compose status

## Environment values you must provide

- `ACCESS_LINK_BASE_URL`: public base URL used to form links like `/access/<token>`
- `PROXY_PUBLIC_HOST`: public host that Chrome should use for SOCKS5
- `BACKEND_PORT`: public backend port, default `18080`
- `PROXY_PUBLIC_PORT`: public SOCKS5 port, default `1080`
- `TOKEN_PEPPER`: HMAC secret for access/session token hashing
- `PROXY_PASSWORD_PEPPER`: HMAC secret for derived proxy passwords

## What you still need to connect on your side

- your real public backend domain
- your real proxy DNS/host
- the exact access-link format used by your Telegram bot
- the actual issuance path so the bot writes valid `access_links` rows instead of relying on the admin CLI

## HTTP status model

- `200`: success
- `400`: malformed request or unsupported mode
- `401`: invalid, missing, revoked, or expired session token
- `403`: invalid or expired access link, or link not allowed to use any node
- `404`: node not found
- `409`: node is offline
- `501`: optional endpoint not implemented yet

## Notes before production

- SOCKS5 does not encrypt the browser-to-proxy hop. This is the main MVP security tradeoff.
- SQLite is acceptable for one-server MVP, but later multi-node deployments should move session state to Postgres or Redis-backed coordination.
- Restrict allowed Chrome extension IDs before a public rollout.
