-- 000010_add_is_game_port.up.sql
-- 标记每个容器配置的"游戏主端口"（对客户端公开的连接端口）。
-- 平台据此从端口映射中确定 connect_address = node_ip:宿主端口。
ALTER TABLE game_container_port_excerpts ADD COLUMN IF NOT EXISTS is_game_port BOOLEAN NOT NULL DEFAULT FALSE;

-- 7DTD: TCP 26900 是游戏端口（与 UDP 26900 同号，TCP/UDP 需映射到同一宿主端口）
UPDATE game_container_port_excerpts SET is_game_port = TRUE
WHERE game_container_config_id = 'cfg-7dtd-demo' AND protocol = 0 AND begin_port = 26900;

-- DST: UDP 10999 是游戏主端口
UPDATE game_container_port_excerpts SET is_game_port = TRUE
WHERE game_container_config_id = 'cfg-dst-demo' AND protocol = 1 AND begin_port = 10999;
