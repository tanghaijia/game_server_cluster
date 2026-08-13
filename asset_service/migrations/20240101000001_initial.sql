CREATE TABLE IF NOT EXISTS t_asset_service_games (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    app_id VARCHAR(255) NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS t_asset_service_game_builds (
    build_id VARCHAR(255) PRIMARY KEY,
    game_id VARCHAR(255) NOT NULL REFERENCES t_asset_service_games(id),
    channel VARCHAR(255),
    adapter_id VARCHAR(255) NOT NULL,
    adapter_version_major INTEGER NOT NULL,
    adapter_version_minor INTEGER NOT NULL,
    adapter_version_patch INTEGER NOT NULL,
    upstream_version VARCHAR(255),
    artifact_uri TEXT,
    artifact_image_name VARCHAR(255),
    artifact_image_tag VARCHAR(255),
    status VARCHAR(50) NOT NULL,
    pinned BOOLEAN NOT NULL DEFAULT false,
    resolved_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS t_asset_service_snapshot_records (
    snapshot_id VARCHAR(255) PRIMARY KEY,
    instance_id VARCHAR(255) NOT NULL,
    build_id VARCHAR(255),
    snapshot_type VARCHAR(50) NOT NULL,
    instance_data_path TEXT NOT NULL,
    storage_uri TEXT,
    manifest_uri TEXT,
    checksum VARCHAR(255),
    status VARCHAR(50) NOT NULL,
    source_node VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    failure_message TEXT,
    bucket VARCHAR(255) NOT NULL DEFAULT '',
    key VARCHAR(255) NOT NULL DEFAULT '',
    host VARCHAR(255) NOT NULL DEFAULT '',
    host_port INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS t_asset_service_snapshot_latest (
    instance_id VARCHAR(255) PRIMARY KEY,
    snapshot_id VARCHAR(255) NOT NULL REFERENCES t_asset_service_snapshot_records(snapshot_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS t_asset_service_mod_manifests (
    manifest_id VARCHAR(255) PRIMARY KEY,
    game_id VARCHAR(255) NOT NULL,
    config_hash VARCHAR(255) NOT NULL,
    compatibility_note TEXT,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS t_asset_service_mod_entries (
    id SERIAL PRIMARY KEY,
    manifest_id VARCHAR(255) NOT NULL REFERENCES t_asset_service_mod_manifests(manifest_id) ON DELETE CASCADE,
    mod_id VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    required BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS t_asset_service_steam_branches (
    id SERIAL PRIMARY KEY,
    game_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    build_id BIGINT NOT NULL,
    description TEXT,
    app_id VARCHAR(255) NOT NULL,
    UNIQUE(game_id, name)
);

CREATE TABLE IF NOT EXISTS t_asset_service_depot_manifests (
    id SERIAL PRIMARY KEY,
    branch_id INTEGER NOT NULL REFERENCES t_asset_service_steam_branches(id) ON DELETE CASCADE,
    depot_id INTEGER NOT NULL,
    manifest_gid BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS t_asset_service_nodes (
    id VARCHAR(255) PRIMARY KEY,
    ip VARCHAR(255) NOT NULL,
    core_num INTEGER NOT NULL DEFAULT 0,
    core_frequency DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    memory_size BIGINT NOT NULL DEFAULT 0,
    storage_size BIGINT NOT NULL DEFAULT 0,
    location VARCHAR(255) NOT NULL DEFAULT '',
    service_provider VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(50) NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS t_asset_service_node_agents (
    node_id VARCHAR(255) PRIMARY KEY,
    endpoint VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    last_heartbeat_at BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_t_asset_service_game_builds_game_id ON t_asset_service_game_builds(game_id);
CREATE INDEX IF NOT EXISTS idx_t_asset_service_snapshot_records_instance_id ON t_asset_service_snapshot_records(instance_id);
CREATE INDEX IF NOT EXISTS idx_t_asset_service_mod_entries_manifest_id ON t_asset_service_mod_entries(manifest_id);
CREATE INDEX IF NOT EXISTS idx_t_asset_service_depot_manifests_branch_id ON t_asset_service_depot_manifests(branch_id);
CREATE INDEX IF NOT EXISTS idx_t_asset_service_steam_branches_game_id ON t_asset_service_steam_branches(game_id);

-- ── 种子数据 ────────────────────────────────────────────────────────────────

INSERT INTO t_asset_service_games (id, name, app_id)
VALUES ('343050', 'Dont stave together', '343050')
ON CONFLICT (id) DO NOTHING;

INSERT INTO t_asset_service_game_builds (
    build_id, game_id, channel,
    adapter_id, adapter_version_major, adapter_version_minor, adapter_version_patch,
    upstream_version, artifact_uri, artifact_image_name, artifact_image_tag,
    status, pinned, resolved_at, created_at, updated_at
)
SELECT
    '343050-public-0.2.2', '343050', 'public',
    'dst', 0, 1, 0,
    'demo-upstream', 'ccr.ccs.tencentyun.com/cluster_game_server',
    'dst-adapter', '0.2.2',
    'available', true, NOW(), NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM t_asset_service_game_builds WHERE build_id = '343050-public-0.2.2'
);
