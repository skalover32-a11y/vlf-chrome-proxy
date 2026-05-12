# Security Policy

This repository contains a Go backend and HTTPS proxy layer for a Chrome extension workflow that validates Remnawave subscription links and issues short-lived browser proxy sessions.

A formal independent third-party security audit has not yet been completed. Responsible disclosure is welcome.

## Supported Versions and Branches

| Branch or version | Status |
| --- | --- |
| `main` | Supported for current development and security fixes |
| Current deployment built from this repository | Supported when the commit can be identified |
| Older MVP builds | Best-effort only |

TODO: define release tags and support windows.

## Reporting a Vulnerability

- Security contact: TODO: add public security contact
- Report privately with affected endpoint, deployment role and reproduction steps.
- Redact Remnawave API tokens, node registration tokens, subscription links, browser session tokens, proxy credentials, TLS private keys and production hostnames.

Do not publish exploit details or real subscription/session data before coordination.

## Scope

In scope:

- `/browser/*` session and proxy-config API endpoints.
- Remnawave subscription validation.
- Local access-link test mode.
- HTTPS proxy authentication and tunnel handling.
- Node registration and credential validation.
- SQLite persistence and token hashing.
- Docker Compose and Ubuntu installer scripts.

Out of scope unless caused by this code:

- social engineering;
- DDoS against proxy nodes;
- compromise of Remnawave, DNS, TLS CA or Chrome extension distribution;
- weak production secrets after placeholders were not replaced.

## Expected Response Process

Reports are handled best-effort: acknowledge, reproduce, triage, fix or mitigate, and coordinate disclosure. TODO: define SLA and severity levels.

## Safe Harbor

Good-faith testing is welcome when it avoids service disruption, data exfiltration and testing against sessions or subscriptions you do not own.
