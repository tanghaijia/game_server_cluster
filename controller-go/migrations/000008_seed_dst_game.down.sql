-- 000008_seed_dst_game.down.sql
DELETE FROM games WHERE id = '322330';
DELETE FROM game_container_port_excerpts WHERE game_container_config_id = 'cfg-dst-demo';
DELETE FROM game_container_configs WHERE id = 'cfg-dst-demo';
