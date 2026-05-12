# Privacy Overview

This repository implements a backend and HTTPS proxy layer for browser-only proxy sessions.

A formal independent third-party security audit has not yet been completed.

## Data This Component May Process

- Remnawave subscription URLs and short UUIDs.
- Local access-link tokens in test/fallback modes.
- Browser session tokens, stored as HMAC hashes.
- Proxy usernames/passwords and credential metadata.
- Node IDs, node location metadata and proxy host/port details.
- Remnawave subscription status metadata.
- PAC/fixed proxy routing mode and include/bypass domain lists.
- TLS certificate/private key files for HTTPS proxy nodes.
- SQLite records under deployment data paths.

## Data It Should Not Expose

Subscription links, browser session tokens, proxy credentials, node registration tokens, Remnawave API tokens and TLS private keys must not be published in logs, issues or screenshots.

No traffic resale, traffic injection or ad injection is intended by project policy. The proxy forwards browser traffic according to the selected proxy/PAC mode; this statement is not an independently audited fact.

## Logs and Diagnostics

The repository already includes `docs/security-review.md`, which notes redaction of access/subscription links and token-like values. Treat proxy logs and API logs as sensitive until reviewed.

## Third-Party Dependencies

Review `backend/go.mod`, Docker images, SQLite driver behavior, Remnawave API integration and deployment scripts.

## Data Retention

This repository does not define server-side retention policy. See service-level privacy policy.

SQLite rows, logs and TLS files remain according to deployment configuration and cleanup procedures.
