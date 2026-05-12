# Audit Status

A formal independent third-party security audit has not yet been completed.

This document tracks internal audit readiness for the Chrome proxy backend.

## Existing Review Notes

The repository already contains `docs/security-review.md` with implementation notes around token separation, session hashing, proxy credential separation, fail-closed Remnawave validation and remaining risks.

## Review Checklist

- [ ] Browser session token creation, hashing and revocation.
- [ ] Proxy credential derivation and validation.
- [ ] Remnawave fail-closed behavior and cache/TTL assumptions.
- [ ] CORS Chrome extension origin restrictions.
- [ ] Node registration token handling.
- [ ] HTTPS proxy CONNECT validation and destination policy.
- [ ] PAC generation and bypass/include rule safety.
- [ ] SQLite migration and file permissions.
- [ ] TLS private key deployment and renewal hooks.
- [ ] Dependency and Docker image review.

## Future External Audit Plan

TODO: define target commit, central/proxy-node topology, test Remnawave sandbox and browser extension test fixture.
