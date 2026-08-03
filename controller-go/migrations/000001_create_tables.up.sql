-- 000001_create_tables.up.sql
-- 基线迁移：创建 controller-go 全部业务表。
-- 用 IF NOT EXISTS 幂等过渡，兼容之前由 AutoMigrate 建好的库。

CREATE TABLE IF NOT EXISTS games (
    id                  TEXT PRIMARY KEY,
    name                TEXT,
    container_config_id TEXT
);

-- 兼容旧库：早期 AutoMigrate 的 GormGame 模型没有 container_config_id 列
ALTER TABLE games ADD COLUMN IF NOT EXISTS container_config_id TEXT;

CREATE TABLE IF NOT EXISTS nodes (
    id               BIGSERIAL PRIMARY KEY,
    ip               TEXT,
    core_num         INTEGER,
    core_frequency   DOUBLE PRECISION,
    memory_size      BIGINT,
    storage_size     BIGINT,
    location         TEXT,
    service_provider TEXT
);

CREATE TABLE IF NOT EXISTS node_agents (
    id      TEXT PRIMARY KEY,
    node_id TEXT,
    port    INTEGER
);

CREATE TABLE IF NOT EXISTS game_instances (
    id                TEXT PRIMARY KEY,
    game_id           TEXT,
    node_agent_id     TEXT,
    status            INTEGER,
    last_pending_time TIMESTAMPTZ,
    create_time       TIMESTAMPTZ,
    update_time       TIMESTAMPTZ,
    game_build_id     TEXT
);

CREATE TABLE IF NOT EXISTS game_container_configs (
    id                    TEXT PRIMARY KEY,
    container_server_path TEXT,
    port_mode             INTEGER
);

-- 容器端口片段：描述游戏所需的一段连续容器端口（NAT 模式下动态映射到宿主机端口）
CREATE TABLE IF NOT EXISTS game_container_port_excerpts (
    id                       BIGSERIAL PRIMARY KEY,
    game_container_config_id TEXT,
    protocol                 INTEGER,
    begin_port               INTEGER,
    excerpt_length           INTEGER
);

CREATE INDEX IF NOT EXISTS idx_game_container_port_excerpts_config_id
    ON game_container_port_excerpts (game_container_config_id);

-- 实例级运行时端口映射：调度阶段为 instance 在 node_agent 上动态分配的宿主端口
CREATE TABLE IF NOT EXISTS game_container_port_mappings (
    id             TEXT PRIMARY KEY,
    instance_id    TEXT,
    node_agent_id  TEXT,
    host_port      INTEGER,
    container_port INTEGER,
    protocol       INTEGER
);

CREATE INDEX IF NOT EXISTS idx_game_container_port_mappings_instance_id
    ON game_container_port_mappings (instance_id);

CREATE INDEX IF NOT EXISTS idx_game_container_port_mappings_node_agent_id
    ON game_container_port_mappings (node_agent_id);
