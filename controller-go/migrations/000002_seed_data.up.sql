-- 000002_seed_data.up.sql
-- 预制数据：demo game + 容器配置 + 节点 + node_agent。
-- 值均为 demo 占位（路径/IP/端口按需修改）；主键冲突用 ON CONFLICT 幂等。

-- 容器配置（port_mode: 0=NAT, 1=HOST）
INSERT INTO game_container_configs (id, container_server_path, port_mode)
VALUES ('cfg-dst-demo', '/server', 0)
ON CONFLICT (id) DO NOTHING;

-- 端口片段（protocol: 0=TCP, 1=UDP；begin_port + excerpt_length 描述一段连续容器端口）
INSERT INTO game_container_port_excerpts (game_container_config_id, protocol, begin_port, excerpt_length)
VALUES
    ('cfg-dst-demo', 1, 10999, 2),
    ('cfg-dst-demo', 1, 8768,  2),
    ('cfg-dst-demo', 1, 27018, 2)
ON CONFLICT DO NOTHING;

-- demo game（沿用 asset_service 的 DST 343050）
INSERT INTO games (id, name, container_config_id)
VALUES ('343050', 'Dont stave together', 'cfg-dst-demo')
ON CONFLICT (id) DO NOTHING;

-- 节点
INSERT INTO nodes (id, ip, core_num, core_frequency, memory_size, storage_size, location, service_provider)
VALUES (1, '127.0.0.1', 4, 3.2, 16384, 102400, 'local', 'demo')
ON CONFLICT (id) DO NOTHING;

-- 修复 bigserial 序列，避免后续自动插入主键冲突
SELECT setval('nodes_id_seq', (SELECT COALESCE(MAX(id), 1) FROM nodes));

-- node_agent
INSERT INTO node_agents (id, node_id, port)
VALUES ('node-agent-1', '1', 50052)
ON CONFLICT (id) DO NOTHING;
