# Central Node Deployment

This guide deploys the central VLF Proxy backend without building Docker images on the server.

## 1. Prepare DNS

Create a DNS record for the central backend, for example:

```text
api.example.com -> <central-server-ip>
```

The backend listens on `BACKEND_PORT`, default `18080`. Put it behind your reverse proxy/CDN if you want public HTTPS on `443`.

## 2. Run Installer

On a clean Ubuntu server:

```bash
NODE_ROLE=central \
BACKEND_PORT=18080 \
bash -c "$(curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh)"
```

The installer:

- installs Docker and Docker Compose if missing
- clones or updates `/opt/vlf-chrome-proxy`
- creates `/opt/vlf-chrome-proxy/.env`
- generates `NODE_REGISTRATION_TOKEN`
- pulls prebuilt GHCR Docker images
- starts only the central `api` container

## 3. Configure Production Values

Edit:

```bash
nano /opt/vlf-chrome-proxy/.env
```

Set at least:

```env
NODE_ROLE=central
BACKEND_PORT=18080
ACCESS_SOURCE_MODE=remna_or_local
REMNA_API_BASE_URL=https://dev.maloff32.tech
REMNA_API_TOKEN=<your-remna-api-token>
ACCESS_LINK_BASE_URL=https://subv2.clearforfun.tech
CORS_ALLOWED_ORIGINS=
ALLOWED_CHROME_EXTENSION_IDS=<chrome-extension-id-after-publish>
```

Keep this value. It is required when installing proxy nodes:

```bash
grep NODE_REGISTRATION_TOKEN /opt/vlf-chrome-proxy/.env
```

Restart after edits:

```bash
cd /opt/vlf-chrome-proxy
docker compose --env-file .env -p vlf_chrome_proxy up -d --no-build api
```

## 4. Check Health

```bash
cd /opt/vlf-chrome-proxy
docker compose --env-file .env -p vlf_chrome_proxy ps
docker compose --env-file .env -p vlf_chrome_proxy logs -f api
```

Expected API port check:

```bash
curl -i http://127.0.0.1:18080/healthz
```

## 5. Install Proxy Nodes

On each proxy server, use the token from the central `.env`:

```bash
NODE_ROLE=proxy_node \
CENTRAL_BACKEND_URL=https://api.example.com \
NODE_REGISTRATION_TOKEN='<token-from-central-env>' \
NODE_DEFAULT_ID=de-1 \
NODE_DEFAULT_NAME='Germany #1' \
NODE_DEFAULT_COUNTRY=DE \
NODE_DEFAULT_CITY=Frankfurt \
PROXY_PUBLIC_HOST=de1.example.com \
PROXY_PUBLIC_PORT=1443 \
bash -c "$(curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/vlf-chrome-proxy/main/deploy/ubuntu/install.sh)"
```

After the proxy node starts, it registers itself in the central backend. The Chrome extension receives the node in `nodes[]` after session revalidation or popup reopen.

## Notes

- Default deployment pulls prebuilt images from GHCR and does not compile Go on the server.
- To force server-side builds, run with `BUILD_ON_SERVER=true`.
- If GHCR images are private, make them public in GitHub Packages or pass `GHCR_USERNAME` and `GHCR_TOKEN`.
