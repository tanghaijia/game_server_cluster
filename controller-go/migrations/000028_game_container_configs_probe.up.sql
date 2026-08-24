-- 000028_game_container_configs_probe.up.sql
-- B-04/P1-3：游戏容器配置声明运行时探针
--   probe_mode: "script" | "a2s" | "none"（缺省 script，向后兼容）
--   query_port_offset: a2s 模式查询端口相对游戏宿主端口的偏移（Valheim=1，多数=0）
ALTER TABLE game_container_configs ADD COLUMN probe_mode TEXT NOT NULL DEFAULT 'script';
ALTER TABLE game_container_configs ADD COLUMN query_port_offset INT NOT NULL DEFAULT 0;
