-- 000010_add_is_game_port.down.sql
ALTER TABLE game_container_port_excerpts DROP COLUMN IF EXISTS is_game_port;
