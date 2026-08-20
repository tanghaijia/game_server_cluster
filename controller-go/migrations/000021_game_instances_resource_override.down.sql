-- 000021_game_instances_resource_override.down.sql

ALTER TABLE game_instances DROP COLUMN IF EXISTS resource_override;
