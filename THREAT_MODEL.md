# Threat Model

A formal independent third-party security audit has not yet been completed.

## Assets

- Remnawave subscription links and short UUIDs.
- Browser session tokens and HMAC token hashes.
- Proxy usernames/passwords.
- Node registration tokens.
- Remnawave API token.
- TLS private keys for HTTPS proxy nodes.
- SQLite database and runtime node config.
- PAC routing rules and proxy include/bypass domains.

## Threat Actors

- Anonymous web attackers.
- Users with leaked subscription links.
- Attackers with leaked browser session tokens or proxy credentials.
- Malicious or compromised proxy nodes.
- Compromised Remnawave/API provider or dependency.
- Operators accidentally exposing `.env`, SQLite DB or TLS keys.

## Threats

- Subscription link leakage.
- Browser session hijacking.
- Proxy credential replay or sharing.
- Node registration abuse.
- PAC/routing misconfiguration causing unintended direct/proxy routing.
- TLS private key compromise.
- Dependency/build compromise.
- Logging of token-like values.

## Mitigations Observed

- Session and access tokens are stored as hashes.
- Proxy credentials are separated from subscription links and browser session tokens.
- Remnawave failures are documented as fail-closed for session revalidation.
- HTTPS proxy uses TLS and Basic proxy auth.
- Existing security review notes redaction and remaining risks.

## Recommended Improvements

- Add rate limiting for exchange/session/proxy auth flows.
- Pin production Chrome extension IDs.
- Add audit logs and alerting for revokes and proxy auth failures.
- Document token and TLS key rotation.
- Review immediate revocation requirements for high-risk deployments.

## Non-Goals

- Auditing Chrome extension distribution itself.
- DDoS resistance for public proxy nodes.
- Service-level retention policy outside this repository.
