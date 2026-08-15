-- 000008_seed_dst_game.up.sql
-- 预制 Don't Starve Together（Steam AppID 322330）demo 数据：
--   容器配置 + 端口片段 + games 行（与 7DTD 的 000007 对齐；ON CONFLICT 幂等）

-- 容器配置（port_mode: 0=NAT, 1=HOST）
INSERT INTO game_container_configs (id, container_server_path, port_mode)
VALUES ('cfg-dst-demo', '/server', 0)
ON CONFLICT (id) DO NOTHING;

-- 端口片段（protocol: 0=TCP, 1=UDP）
INSERT INTO game_container_port_excerpts (game_container_config_id, protocol, begin_port, excerpt_length)
VALUES
    ('cfg-dst-demo', 1, 10999, 1),  -- UDP 10999 游戏端口（server_port）
    ('cfg-dst-demo', 1, 27016, 1)   -- UDP 27016 Steam 查询端口
ON CONFLICT DO NOTHING;

-- Don't Starve Together 游戏（id 即 Steam AppID）
INSERT INTO games (id, name, container_config_id, app_id)
VALUES ('322330', 'Don''t Starve Together', 'cfg-dst-demo', '322330')
ON CONFLICT (id) DO NOTHING;
