-- 000028_game_container_configs_probe.down.sql
ALTER TABLE game_container_configs DROP COLUMN query_port_offset;
ALTER TABLE game_container_configs DROP COLUMN probe_mode;
