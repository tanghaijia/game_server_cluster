-- 000004_update_port_mapping.up.sql
-- 端口映射模型重构：
-- 1. game_container_port_mappings 由「配置级静态映射」改为「实例级运行时映射」，
--    记录调度阶段为 instance 在 node_agent 上动态分配的宿主端口。
-- 2. 新增 game_container_port_excerpts 表，描述容器配置所需的连续端口片段。
-- 旧表里的数据为 demo 静态映射，语义已被替换，直接重建。

DROP TABLE IF EXISTS game_container_port_mappings;

CREATE TABLE game_container_port_mappings (
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

CREATE TABLE IF NOT EXISTS game_container_port_excerpts (
    id                       BIGSERIAL PRIMARY KEY,
    game_container_config_id TEXT,
    protocol                 INTEGER,
    begin_port               INTEGER,
    excerpt_length           INTEGER
);

CREATE INDEX IF NOT EXISTS idx_game_container_port_excerpts_config_id
    ON game_container_port_excerpts (game_container_config_id);

-- 为既有 demo 配置补齐端口片段（幂等，protocol: 0=TCP, 1=UDP）
INSERT INTO game_container_port_excerpts (game_container_config_id, protocol, begin_port, excerpt_length)
SELECT 'cfg-dst-demo', 1, 10999, 2
WHERE NOT EXISTS (SELECT 1 FROM game_container_port_excerpts
                  WHERE game_container_config_id = 'cfg-dst-demo' AND protocol = 1 AND begin_port = 10999);

INSERT INTO game_container_port_excerpts (game_container_config_id, protocol, begin_port, excerpt_length)
SELECT 'cfg-dst-demo', 1, 8768, 2
WHERE NOT EXISTS (SELECT 1 FROM game_container_port_excerpts
                  WHERE game_container_config_id = 'cfg-dst-demo' AND protocol = 1 AND begin_port = 8768);

INSERT INTO game_container_port_excerpts (game_container_config_id, protocol, begin_port, excerpt_length)
SELECT 'cfg-dst-demo', 1, 27018, 2
WHERE NOT EXISTS (SELECT 1 FROM game_container_port_excerpts
                  WHERE game_container_config_id = 'cfg-dst-demo' AND protocol = 1 AND begin_port = 27018);
