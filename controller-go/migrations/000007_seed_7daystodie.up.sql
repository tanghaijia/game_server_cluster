-- 000007_seed_7daystodie.up.sql
-- 预制 7 Days to Die（Steam AppID 294420）demo 数据：
--   容器配置 + 端口片段 + games 行。
-- 与 DST 的 000002_seed_data 对齐；ON CONFLICT 幂等。

-- 容器配置（port_mode: 0=NAT, 1=HOST）
INSERT INTO game_container_configs (id, container_server_path, port_mode)
VALUES ('cfg-7dtd-demo', '/server', 0)
ON CONFLICT (id) DO NOTHING;

-- 端口片段（protocol: 0=TCP, 1=UDP；begin_port + excerpt_length 描述一段连续容器端口）
INSERT INTO game_container_port_excerpts (game_container_config_id, protocol, begin_port, excerpt_length)
VALUES
    ('cfg-7dtd-demo', 0, 26900, 1),  -- TCP 26900 游戏连接
    ('cfg-7dtd-demo', 1, 26900, 3),  -- UDP 26900-26902 游戏数据（LiteNetLib）
    ('cfg-7dtd-demo', 0, 8081,  1)   -- TCP 8081 Telnet（平台 save/shutdown 用）
ON CONFLICT DO NOTHING;

-- 7 Days to Die 游戏（id 即 Steam AppID，node_agent 用其作为 steamcmd app_update 目标）
INSERT INTO games (id, name, container_config_id, app_id)
VALUES ('294420', '7 Days to Die', 'cfg-7dtd-demo', '294420')
ON CONFLICT (id) DO NOTHING;
