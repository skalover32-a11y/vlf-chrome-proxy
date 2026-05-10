# VLF Chrome Proxy Backend MVP

Production-like minimal backend for a Chrome extension that validates Remnawave subscription links, creates short-lived browser sessions, and returns HTTPS proxy credentials.

## What Is Included

- Go backend access/session layer
- HTTPS proxy service with TLS and Basic proxy authentication
- Docker Compose deployment for Ubuntu
- SQLite persistence for MVP
- Remnawave subscription validation for production access
- Admin CLI to create local test access links
- Multi-node data model with node selection by `node_id`
- Fixed proxy and PAC-based Smart Routing

## Architecture

We use one repository and one deployment mode: Docker Compose.

- `api` exposes `/browser/*` endpoints for the Chrome extension.
- `https-proxy` accepts Chrome HTTPS proxy traffic over TLS.
- `/browser/exchange-link` validates Remnawave subscription links in production mode, with local access links kept as a test fallback.
- `/browser/session` and `/browser/proxy-config` re-check the access source; Remnawave failures are fail-closed and revoke the local session.
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

## Access Source Modes

`ACCESS_SOURCE_MODE` controls how `/browser/exchange-link` validates the URL pasted by the user:

- `remna_only`: only Remnawave subscription links are accepted.
- `remna_or_local`: try Remnawave first, then local `access_links` only for fallback/testing.
- `local_only`: only admin-generated local access links are accepted.

Production should use `remna_only` or `remna_or_local`. Local smoke tests can use `local_only`.

### Remnawave Validation

The backend extracts the last path segment from a subscription URL, for example:

```text
https://subv2.example.com/cDLBZDRS82hEmdMW -> cDLBZDRS82hEmdMW
```

It then checks Remnawave:

- primary, when API token is configured: `GET /api/subscriptions/by-short-uuid/{shortUuid}`
- compatibility fallback: `GET /api/sub/{shortUuid}/info`

The expected Remnawave response contains `isFound`, `user.shortUuid`, `user.isActive`, `user.userStatus`, and `user.expiresAt`. A subscription is accepted only when it is found, active, not disabled/revoked, and not expired.

## Data Model

The access/session model keeps local access links for tests, but browser sessions can now point to an external Remnawave subscription.

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
- `access_link_id`: nullable local source access link, used only for local/test mode
- `source_type`: `local_access_link` or `remna_subscription`
- `source_ref`: local access link ID or Remnawave short UUID
- `external_subscription_id`: Remnawave subscription/user identifier snapshot
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

## Multi-Node And Routing

Nodes are configured in `deploy/runtime/nodes.json`. `/browser/exchange-link` and `/browser/session` return the full available `nodes[]` array and `default_node_id`. The extension stores `selected_node_id`; `/browser/proxy-config` and `/browser/pac-config` take `node_id` so switching servers does not change the session model.

For automatic multi-node deployment without a shared database, run one central backend and any number of proxy-only nodes:

- Central server keeps the only SQLite database and runs the browser API.
- Proxy nodes run only `https-proxy`.
- Each proxy node registers itself through `POST /internal/nodes/register` using `NODE_REGISTRATION_TOKEN`.
- Each proxy node validates proxy username/password through `POST /internal/proxy/validate-credentials` on the central backend.
- No SQLite database is shared between servers.

Central server role:

```env
NODE_ROLE=central
NODE_REGISTRATION_TOKEN=<shared-random-secret>
```

Proxy node role:

```env
NODE_ROLE=proxy_node
CENTRAL_BACKEND_URL=https://api.example.com
NODE_REGISTRATION_TOKEN=<same-shared-random-secret>
NODE_DEFAULT_ID=de-1
NODE_DEFAULT_NAME="Germany #1"
NODE_DEFAULT_COUNTRY=DE
NODE_DEFAULT_CITY=Frankfurt
PROXY_PUBLIC_HOST=de1.example.com
PROXY_PUBLIC_PORT=1443
```

One-command proxy node install example:

```bash
NODE_ROLE=proxy_node \
CENTRAL_BACKEND_URL=https://api.example.com \
NODE_REGISTRATION_TOKEN='<same-shared-random-secret>' \
NODE_DEFAULT_ID=de-1 \
NODE_DEFAULT_NAME='Germany #1' \
NODE_DEFAULT_COUNTRY=DE \
NODE_DEFAULT_CITY=Frankfurt \
PROXY_PUBLIC_HOST=de1.example.com \
PROXY_PUBLIC_PORT=1443 \
bash -c "$(curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh)"
```

Interactive proxy node install:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh)"
```

If required values are missing, the installer asks for the install role and writes answers to `.env`. For `central`, it asks for the backend port and generates `NODE_REGISTRATION_TOKEN` automatically. Copy that token from `/opt/vlf-chrome-proxy/.env` and use it on proxy nodes. For a fresh `proxy_node`, it always asks for `CENTRAL_BACKEND_URL`, `NODE_REGISTRATION_TOKEN`, node id/name/country/city, `PROXY_PUBLIC_HOST`, and `PROXY_PUBLIC_PORT` instead of silently accepting template defaults.

For proxy nodes, the installer tries to issue a trusted Let's Encrypt certificate for `PROXY_PUBLIC_HOST` before starting `https-proxy`. Keep DNS pointed to the proxy node and leave port `80/tcp` free for ACME HTTP validation. The installed certificate is copied to `deploy/runtime/tls/proxy.crt` and `deploy/runtime/tls/proxy.key`; a certbot renewal hook refreshes these files and restarts `https-proxy`. If Let's Encrypt fails, the installer falls back to a self-signed bootstrap certificate, which Chrome will reject with `ERR_PROXY_CERTIFICATE_INVALID` until replaced.

After the proxy node starts, it appears in extension `nodes[]` after session revalidation or popup reopen.

Routing modes:

- `fixed_servers`: Full Proxy; all browser traffic goes through the selected HTTPS proxy node.
- `pac_script`: Smart Routing; backend returns a PAC script that proxies only include-list domains and sends the rest direct.
- Proxy include rules come from optional `SMART_ROUTING_PROXY_DOMAINS` plus `/browser/pac-config?proxy=...` values from the extension. Keep `SMART_ROUTING_PROXY_DOMAINS` empty if users should fully control the list from the extension UI.
- Custom bypass rules are passed from the extension as `/browser/pac-config?bypass=...` and are emitted as `DIRECT` PAC rules. Bypass rules win over proxy include rules.

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
- extracts the local token or Remnawave short UUID
- validates the URL according to `ACCESS_SOURCE_MODE`
- creates a short-lived browser session
- creates initial proxy credentials for the default node

### `GET /browser/session`

- validates `Authorization: Bearer <session_token>`
- checks TTL and revoke state
- checks the local access link or Remnawave subscription is still active
- returns current node list
- returns `subscription` status metadata

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

### `GET /browser/pac-config?node_id=node-1&proxy=youtube.com&bypass=example.com`

Returns a Smart Routing PAC config plus proxy auth credentials for the selected node. `proxy` is a comma-separated include-list of domains that should go through the proxy; all other traffic is direct. `bypass` is a comma-separated exception list that is forced direct even if it also matches the proxy include-list. The proxy credentials stay temporary and session-bound, same as Full Proxy.

## Optional Endpoints In This MVP

- `POST /browser/logout`: implemented and revokes the session plus proxy credentials
- `GET /browser/ip`: stub, returns `501`; the extension treats this as optional
- `GET /browser/pac-config`: implemented for Smart Routing

## Data Flow

```text
access link URL
  -> POST /browser/exchange-link
  -> Remnawave subscription validation or local fallback validation
  -> browser session created with source_type/source_ref
  -> session_token returned
  -> GET /browser/session
  -> GET /browser/proxy-config?node_id=... or GET /browser/pac-config?node_id=...
  -> Chrome applies HTTPS fixed proxy or PAC Smart Routing
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

For `NODE_ROLE=proxy_node`, the installer attempts to obtain a Let's Encrypt certificate for `PROXY_PUBLIC_HOST` automatically using certbot standalone mode. Requirements:

- `PROXY_PUBLIC_HOST` must be a real DNS name, not an IP address.
- DNS must point to the proxy node public IP.
- `80/tcp` must be reachable and not occupied during issuance.
- `1443/tcp` must be reachable for Chrome HTTPS proxy traffic.

The installer creates a 30-day self-signed certificate only as a bootstrap fallback. If Chrome shows `ERR_PROXY_CERTIFICATE_INVALID`, the proxy node is still using the fallback certificate.

## Sensitive Fields

Treat these as secrets:

- raw access-link token
- full access-link URL
- Remnawave subscription URL / short UUID
- `REMNA_API_TOKEN`
- `session_token`
- proxy `username`
- proxy `password`
- TLS private key
- `TOKEN_PEPPER`
- `PROXY_PASSWORD_PEPPER`

The backend never logs raw access links, raw Remnawave API token, or raw session tokens.

## First Local Run

1. Copy the environment template:
   `cp .env.example .env`
2. Edit at least:
   - `TOKEN_PEPPER`
   - `PROXY_PASSWORD_PEPPER`
   - `ACCESS_LINK_BASE_URL`
   - `PROXY_PUBLIC_HOST`
   - `ACCESS_SOURCE_MODE`
   - `REMNA_API_BASE_URL` and `REMNA_API_TOKEN` if using Remnawave mode
3. Create runtime directories:
   `mkdir -p deploy/data deploy/runtime/tls`
4. Place trusted TLS cert/key into `deploy/runtime/tls/proxy.crt` and `deploy/runtime/tls/proxy.key`, or let the installer create a self-signed smoke-test cert.
5. Copy the node template:
   `cp configs/templates/nodes.json deploy/runtime/nodes.json`
6. Start the stack:
   `docker compose up -d --build`
7. For local-only testing, set `ACCESS_SOURCE_MODE=local_only` and create a test access link:
   `docker compose --profile tools run --rm admin create-access-link --label local-test --default-node node-1 --expires-in 24h`
8. For Remnawave testing, set `ACCESS_SOURCE_MODE=remna_or_local` or `remna_only` and paste a real Remnawave subscription URL into the Chrome extension.

## One-Command Ubuntu Deployment

On a clean Ubuntu host:

```bash
curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh | bash
```

By default the installer does not build Go/Docker images on the server. It pulls prebuilt images from GHCR:

- `ghcr.io/skalover32-a11y/vlf-chrome-proxy-api:main`
- `ghcr.io/skalover32-a11y/vlf-chrome-proxy-https-proxy:main`
- `ghcr.io/skalover32-a11y/vlf-chrome-proxy-admin:main`

Use server-side builds only when explicitly needed:

```bash
BUILD_ON_SERVER=true bash -c "$(curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh)"
```

If GHCR packages are private, either make the container packages public in GitHub Packages or pass `GHCR_USERNAME` and `GHCR_TOKEN` when running the installer.

Central backend install without building on the server:

```bash
NODE_ROLE=central \
BACKEND_PORT=18080 \
bash -c "$(curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh)"
```

What the installer does:

- installs Docker Engine and Docker Compose plugin if missing
- clones or updates the repository to `/opt/vlf-chrome-proxy`
- creates `.env` from `.env.example` if needed
- migrates old SOCKS5 env/node defaults to HTTPS proxy defaults
- creates runtime directories
- generates `deploy/runtime/nodes.json` from env if needed for `all_in_one`
- requests a Let's Encrypt proxy certificate for proxy roles, falling back to self-signed only if issuance fails
- pulls prebuilt Docker images and starts only the services required by `NODE_ROLE`
- can still build locally when `BUILD_ON_SERVER=true`
- removes old compose orphan containers
- optionally creates a bootstrap access link
- prints compose status

## Environment Values You Must Provide

- `ACCESS_LINK_BASE_URL`: public base URL used to form links like `/access/<token>`
- `ACCESS_SOURCE_MODE`: `remna_only`, `remna_or_local`, or `local_only`
- `BUILD_ON_SERVER`: `false` by default; use prebuilt GHCR images instead of compiling on VPS
- `IMAGE_TAG`: image tag to pull, default `main`
- `REMNA_API_BASE_URL`: Remnawave panel API origin, for example `https://dev.example.com`
- `REMNA_API_TOKEN`: Remnawave API bearer token; keep it secret
- `REMNA_TIMEOUT_SECONDS`: Remnawave API request timeout, default `10`
- `REMNA_ALLOW_INSECURE_TLS`: dev-only TLS verification bypass, default `false`
- `SMART_ROUTING_PROXY_DOMAINS`: optional comma-separated server-side domains always routed through proxy in PAC mode. Default is empty; do not put IP-check domains like `2ip.ru` here unless you intentionally want them proxied.
- `NODE_ROLE`: `all_in_one`, `central`, or `proxy_node`; default `all_in_one`
- `CENTRAL_BACKEND_URL`: central backend URL used by proxy-only nodes
- `NODE_REGISTRATION_TOKEN`: shared secret for proxy node registration and remote credential validation
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
- your Remnawave panel URL and API token
- the subscription URL format your bot gives to users
- whether production should use `remna_only` or `remna_or_local`

## Manual Smoke Checks

Check backend:

```bash
curl -fsS http://127.0.0.1:18080/healthz
```

Check Remnawave exchange:

```bash
curl -fsS -X POST http://127.0.0.1:18080/browser/exchange-link \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://subv2.example.com/SHORT_UUID"}'
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
- `502`: Remnawave API auth/config problem
- `503`: Remnawave API unavailable or no nodes configured
- `404`: node not found
- `409`: node is offline
- `501`: optional endpoint not implemented yet

## Notes Before Production

- Use a trusted TLS certificate for the proxy host before real Chrome testing.
- SQLite is acceptable for one-server MVP, but later multi-node deployments should move session state to Postgres or Redis-backed coordination.
- Restrict allowed Chrome extension IDs before a public rollout.
- Add rate limiting for `/browser/exchange-link` before public traffic.
- Keep `REMNA_API_TOKEN` out of shell history, logs, screenshots, and committed files.
