-- 000011_add_port_injection.down.sql
ALTER TABLE game_container_configs DROP COLUMN IF EXISTS inject_game_port;
ALTER TABLE game_container_port_mappings DROP COLUMN IF EXISTS is_game_port;
