# Manual Test Plan

## Backend Happy Path

1. Run the installer or `docker compose up -d --build`.
2. Confirm services are running with `docker compose --env-file .env -p vlf_chrome_proxy ps`.
3. Create a bootstrap access link with `docker compose --env-file .env -p vlf_chrome_proxy --profile tools run --rm admin create-access-link --label test --default-node node-1 --expires-in 24h`.
4. Call `POST /browser/exchange-link` with the returned URL.
5. Confirm response contains `session_token`, `nodes`, `default_node_id=node-1`, `default_mode=fixed_servers`.
6. Call `GET /browser/session` with `Authorization: Bearer <session_token>`.
7. Call `GET /browser/proxy-config?node_id=node-1&mode=fixed_servers` with the same bearer token.
8. Verify the returned `scheme=https`, `port=1443`, `username`, and `password`.

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
7. Click `Disconnect` and confirm browser traffic returns to direct mode.

## Error Handling

1. Exchange with malformed JSON and confirm HTTP `400`.
2. Exchange with an unknown access-link URL and confirm HTTP `403`.
3. Use an expired access link and confirm HTTP `403`.
4. Use an invalid `session_token` on `/browser/session` and confirm HTTP `401`.
5. Use an expired `session_token` and confirm HTTP `401`.
6. Request `/browser/proxy-config` with an unknown `node_id` and confirm HTTP `404`.
7. Mark the node offline in `deploy/runtime/nodes.json`, restart the stack, and confirm `/browser/proxy-config` returns HTTP `409`.

## Logout And Optional Endpoints

1. Call `POST /browser/logout` with a valid `session_token` and confirm HTTP `200`.
2. Reuse the same `session_token` and confirm `/browser/session` now returns HTTP `401`.
3. Call `GET /browser/ip` and confirm HTTP `501`; extension UI should show `IP unavailable` without global error.
4. Call `GET /browser/pac-config` and confirm HTTP `501`.
