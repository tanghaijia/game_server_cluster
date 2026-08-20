-- 000020_game_instances_fail_reason.down.sql

ALTER TABLE game_instances DROP COLUMN IF EXISTS fail_reason;
