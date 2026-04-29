# Security Review

## Current MVP posture

- Access link is used only once for `POST /browser/exchange-link`; it is never returned back to the extension.
- `session_token` is separate from the access link and is stored only as an HMAC hash in SQLite.
- Proxy credentials are separate from both the access link and the session token.
- Proxy passwords are derived server-side from secret material and are not stored in plaintext.
- `revoked_at` and `expires_at` exist on `access_links`, `browser_sessions`, and `proxy_credentials`.
- Logs redact access links, proxy usernames, and other token-like values.

## Important MVP risks

- SOCKS5 does not encrypt the browser-to-proxy hop. Username/password auth and traffic metadata are exposed to the network path unless you add an outer secure transport or place the proxy inside a trusted network.
- SQLite is fine for a single-node MVP, but it is not the right long-term store for multi-host coordination.
- CORS is intentionally permissive for Chrome extension development when `CORS_ALLOW_CHROME_EXTENSION_ORIGINS=true`. For production you should restrict `ALLOWED_CHROME_EXTENSION_IDS`.
- Access-link issuance is still your responsibility. The current MVP includes an admin CLI for manual creation, but your Telegram bot or billing backend must insert or mint real access links in production.

## Recommended next hardening steps

- Restrict SOCKS5 exposure with a firewall to only the client regions or IP ranges you expect.
- Pin allowed Chrome extension IDs in `ALLOWED_CHROME_EXTENSION_IDS`.
- Move from manual/admin-generated access links to a signed server-side issuance flow from your existing bot backend.
- Add rate limiting for `/browser/exchange-link` and `/browser/session`.
- Add audit logs for revoke events and proxy auth failures with aggregation/alerting.
