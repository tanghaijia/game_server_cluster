-- 000029_game_instances_cache_warming.down.sql

ALTER TABLE game_instances DROP COLUMN IF EXISTS cache_build_id;
ALTER TABLE game_instances DROP COLUMN IF EXISTS branch_name;
