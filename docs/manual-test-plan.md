# Manual Test Plan

## Remnawave Backend Happy Path

1. Run the installer or `docker compose up -d --build`.
2. Confirm services are running with `docker compose --env-file .env -p vlf_chrome_proxy ps`.
3. Set `ACCESS_SOURCE_MODE=remna_or_local` or `ACCESS_SOURCE_MODE=remna_only`.
4. Set `REMNA_API_BASE_URL` to the Remnawave panel origin and `REMNA_API_TOKEN` to a valid API token.
5. Call `POST /browser/exchange-link` with a real Remnawave subscription URL.
6. Confirm response contains `session_token`, `nodes`, `default_node_id=node-1`, `default_mode=fixed_servers`.
7. Call `GET /browser/session` with `Authorization: Bearer <session_token>`.
8. Call `GET /browser/proxy-config?node_id=node-1&mode=fixed_servers` with the same bearer token.
9. Verify the returned `scheme=https`, `port=1443`, `username`, and `password`.
10. Call `GET /browser/pac-config?node_id=node-1&bypass=example.com` and verify `mode=pac_script`, `pac_script`, `username`, and `password`.

## Local Fallback Happy Path

1. Set `ACCESS_SOURCE_MODE=local_only` or `ACCESS_SOURCE_MODE=remna_or_local`.
2. Create a bootstrap access link with `docker compose --env-file .env -p vlf_chrome_proxy --profile tools run --rm admin create-access-link --label test --default-node node-1 --expires-in 24h`.
3. Call `POST /browser/exchange-link` with the returned URL.
4. Confirm response contains `session_token`, `nodes`, `default_node_id=node-1`, `default_mode=fixed_servers`.
5. Call `GET /browser/session` with `Authorization: Bearer <session_token>`.
6. Call `GET /browser/proxy-config?node_id=node-1&mode=fixed_servers` with the same bearer token.
7. Verify the returned `scheme=https`, `port=1443`, `username`, and `password`.

## HTTPS Proxy Checks

1. Confirm TLS is reachable with `openssl s_client -connect proxy.example.com:1443 -servername proxy.example.com </dev/null`.
2. Confirm proxy auth fails without credentials with `curl -vk -x https://proxy.example.com:1443 https://api.ipify.org`.
3. Confirm proxy auth works with issued credentials using `curl -vk -x https://browser_u_xxx:browser_p_xxx@proxy.example.com:1443 https://api.ipify.org`.

## Chrome Extension Checks

1. Load the extension from `dist` in Chrome Developer Mode.
2. Paste the access link and activate.
3. Click `Connect`.
4. Confirm Chrome requests optional proxy auth permissions if needed.
5. Confirm popup status becomes `Connected`.
6. Open a website in Chrome and confirm traffic works through the proxy.
7. Switch server in the popup; if connected, confirm it reconnects with the selected node.
8. Switch routing mode from `Full Proxy` to `Smart Routing`; confirm PAC mode applies without losing the session.
9. Add a custom bypass rule and confirm it persists after reopening popup/options.
10. Click `Disconnect` and confirm browser traffic returns to direct mode.

## Error Handling

1. Exchange with malformed JSON and confirm HTTP `400`.
2. Exchange with an unknown Remnawave subscription URL and confirm HTTP `403`.
3. Use an expired/disabled Remnawave subscription and confirm HTTP `403`.
4. Stop or misconfigure Remnawave API and confirm HTTP `502` or `503`.
5. Exchange with an unknown local access-link URL in `local_only` mode and confirm HTTP `403`.
6. Use an invalid `session_token` on `/browser/session` and confirm HTTP `401`.
7. Use an expired `session_token` and confirm HTTP `401`.
8. Request `/browser/proxy-config` with an unknown `node_id` and confirm HTTP `404`.
9. Mark the node offline in `deploy/runtime/nodes.json`, restart the stack, and confirm `/browser/proxy-config` returns HTTP `409`.
10. Disable or expire the Remnawave subscription, call `/browser/session`, and confirm the session is rejected and local proxy credentials stop working after reconnect/revalidation.

## Logout And Optional Endpoints

1. Call `POST /browser/logout` with a valid `session_token` and confirm HTTP `200`.
2. Reuse the same `session_token` and confirm `/browser/session` now returns HTTP `401`.
3. Call `GET /browser/ip` and confirm HTTP `501`; this endpoint is optional in the MVP.
4. Call `GET /browser/pac-config` without a valid bearer token and confirm HTTP `401`.
