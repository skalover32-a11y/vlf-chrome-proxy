# Manual Test Plan

## Happy path

1. Run the installer or `docker compose up -d --build`.
2. Create a bootstrap access link with:
   `docker compose --profile tools run --rm admin create-access-link --label test --default-node node-1 --expires-in 24h`
3. Call `POST /browser/exchange-link` with the returned URL.
4. Confirm response contains `session_token`, `nodes`, `default_node_id=node-1`, `default_mode=fixed_servers`.
5. Call `GET /browser/session` with `Authorization: Bearer <session_token>`.
6. Call `GET /browser/proxy-config?node_id=node-1&mode=fixed_servers` with the same bearer token.
7. Verify the returned `host`, `port`, `scheme=socks5`, `username`, and `password`.
8. Configure the Chrome extension with the returned config and verify traffic goes through the SOCKS5 proxy.

## Error handling

1. Exchange with malformed JSON and confirm HTTP `400`.
2. Exchange with an unknown access-link URL and confirm HTTP `403`.
3. Use an expired access link and confirm HTTP `403`.
4. Use an invalid `session_token` on `/browser/session` and confirm HTTP `401`.
5. Use an expired `session_token` and confirm HTTP `401`.
6. Request `/browser/proxy-config` with an unknown `node_id` and confirm HTTP `404`.
7. Mark the node offline in `deploy/runtime/nodes.json`, restart the stack, and confirm `/browser/proxy-config` returns HTTP `409`.

## Logout and stubs

1. Call `POST /browser/logout` with a valid `session_token` and confirm HTTP `200`.
2. Reuse the same `session_token` and confirm `/browser/session` now returns HTTP `401`.
3. Call `GET /browser/ip` and confirm HTTP `501`.
4. Call `GET /browser/pac-config` and confirm HTTP `501`.
