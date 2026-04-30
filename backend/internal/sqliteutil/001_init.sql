CREATE TABLE IF NOT EXISTS access_links (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    status TEXT NOT NULL DEFAULT 'active',
    allowed_node_ids TEXT NOT NULL DEFAULT '[]',
    default_node_id TEXT,
    expires_at TEXT NOT NULL,
    last_exchanged_at TEXT,
    revoked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    country TEXT NOT NULL,
    city TEXT NOT NULL,
    host TEXT NOT NULL,
    proxy_port INTEGER NOT NULL,
    proxy_scheme TEXT NOT NULL,
    supports_pac INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'online',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    is_default INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS browser_sessions (
    id TEXT PRIMARY KEY,
    access_link_id TEXT,
    source_type TEXT NOT NULL DEFAULT 'local_access_link',
    source_ref TEXT,
    external_subscription_id TEXT,
    session_token_hash TEXT NOT NULL UNIQUE,
    selected_node_id TEXT,
    default_node_id TEXT,
    available_node_ids TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TEXT NOT NULL,
    last_seen_at TEXT,
    revoked_at TEXT,
    client_ip TEXT,
    user_agent TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (access_link_id) REFERENCES access_links(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS proxy_credentials (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    username TEXT NOT NULL UNIQUE,
    password_version INTEGER NOT NULL DEFAULT 1,
    expires_at TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (session_id, node_id),
    FOREIGN KEY (session_id) REFERENCES browser_sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_access_links_token_hash ON access_links(token_hash);
CREATE INDEX IF NOT EXISTS idx_browser_sessions_token_hash ON browser_sessions(session_token_hash);
CREATE INDEX IF NOT EXISTS idx_proxy_credentials_username ON proxy_credentials(username);
