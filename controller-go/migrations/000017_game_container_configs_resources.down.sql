-- 000017_game_container_configs_resources.down.sql

ALTER TABLE game_container_configs DROP COLUMN IF EXISTS cpu_request_milli;
ALTER TABLE game_container_configs DROP COLUMN IF EXISTS memory_request_bytes;
ALTER TABLE game_container_configs DROP COLUMN IF EXISTS disk_request_bytes;
ALTER TABLE game_container_configs DROP COLUMN IF EXISTS bandwidth_rx_mbps;
ALTER TABLE game_container_configs DROP COLUMN IF EXISTS bandwidth_tx_mbps;
ALTER TABLE game_container_configs DROP COLUMN IF EXISTS single_threaded;
