# Security Review

## Current MVP Posture

- Access link is used only for `POST /browser/exchange-link`; it is never used as a proxy password.
- `session_token` is separate from the access link and is stored only as an HMAC hash in SQLite.
- HTTPS proxy credentials are separate from both the access link and the session token.
- Proxy passwords are derived server-side from secret material and are not stored in plaintext.
- `revoked_at` and `expires_at` exist on `access_links`, `browser_sessions`, and `proxy_credentials`.
- Logs redact access links, proxy usernames, and other token-like values.
- Chrome connects to the proxy over TLS when backend returns `scheme: "https"`.

## Important MVP Risks

- The HTTPS proxy private key must be protected. It is mounted from `deploy/runtime/tls/proxy.key`.
- The installer creates a self-signed bootstrap certificate if no cert exists. That is useful for smoke tests, but real Chrome testing needs a trusted certificate for `PROXY_PUBLIC_HOST`.
- SQLite is fine for a single-node MVP, but it is not the right long-term store for multi-host coordination.
- CORS is intentionally permissive for Chrome extension development when `CORS_ALLOW_CHROME_EXTENSION_ORIGINS=true`. For production you should restrict `ALLOWED_CHROME_EXTENSION_IDS`.
- Access-link issuance is still your responsibility. The current MVP includes an admin CLI for manual creation, but your Telegram bot or billing backend must insert or mint real access links in production.

## Recommended Next Hardening Steps

- Replace bootstrap TLS material with a trusted certificate and secure private key permissions.
- Restrict inbound proxy traffic with a firewall where possible.
- Pin allowed Chrome extension IDs in `ALLOWED_CHROME_EXTENSION_IDS`.
- Move from manual/admin-generated access links to a signed server-side issuance flow from your existing bot backend.
- Add rate limiting for `/browser/exchange-link`, `/browser/session`, and proxy auth failures.
- Add audit logs for revoke events and proxy auth failures with aggregation/alerting.
