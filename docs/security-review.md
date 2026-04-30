# Security Review

## Current MVP Posture

- Remnawave subscription link or local access link is used only for `POST /browser/exchange-link`; it is never used as a proxy password.
- `session_token` is separate from the access link and is stored only as an HMAC hash in SQLite.
- HTTPS proxy credentials are separate from the subscription link, local access link, and session token.
- Proxy passwords are derived server-side from secret material and are not stored in plaintext.
- `revoked_at` and `expires_at` exist on local `access_links`, `browser_sessions`, and `proxy_credentials`; Remnawave subscription expiry is checked through Remnawave on exchange/session/proxy-config/PAC.
- Remnawave invalid/expired/revoked responses are fail-closed and revoke the local browser session.
- PAC Smart Routing still uses backend-issued temporary proxy credentials; PAC rules never contain access links or session tokens.
- Logs redact access/subscription links, proxy usernames, and other token-like values.
- Chrome connects to the proxy over TLS when backend returns `scheme: "https"`.

## Important MVP Risks

- The HTTPS proxy private key must be protected. It is mounted from `deploy/runtime/tls/proxy.key`.
- The installer creates a self-signed bootstrap certificate if no cert exists. That is useful for smoke tests, but real Chrome testing needs a trusted certificate for `PROXY_PUBLIC_HOST`.
- SQLite is fine for a single-node MVP, but it is not the right long-term store for multi-host coordination.
- CORS is intentionally permissive for Chrome extension development when `CORS_ALLOW_CHROME_EXTENSION_ORIGINS=true`. For production you should restrict `ALLOWED_CHROME_EXTENSION_IDS`.
- `REMNA_API_TOKEN` is a high-value secret. It must not be pasted into shell history, committed files, screenshots, or logs.
- `ValidateProxyCredentials` intentionally avoids calling Remnawave on every proxy auth challenge. Revokes are enforced when the extension validates session/proxy-config/PAC and by short local TTLs. If immediate proxy-level revoke is required, add a short Remnawave cache or revocation sync worker.
- Local `access_links` are retained only for fallback/test mode. Production should use `ACCESS_SOURCE_MODE=remna_only` or a controlled `remna_or_local`.

## Recommended Next Hardening Steps

- Replace bootstrap TLS material with a trusted certificate and secure private key permissions.
- Restrict inbound proxy traffic with a firewall where possible.
- Pin allowed Chrome extension IDs in `ALLOWED_CHROME_EXTENSION_IDS`.
- Rotate the Remnawave API token if it was exposed during manual setup.
- Use `ACCESS_SOURCE_MODE=remna_only` once local fallback is no longer needed.
- Add rate limiting for `/browser/exchange-link`, `/browser/session`, and proxy auth failures.
- Add audit logs for revoke events and proxy auth failures with aggregation/alerting.
