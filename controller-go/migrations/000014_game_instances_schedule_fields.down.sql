-- 000014_game_instances_schedule_fields.down.sql

ALTER TABLE game_instances DROP COLUMN IF EXISTS region;
ALTER TABLE game_instances DROP COLUMN IF EXISTS priority;
ALTER TABLE game_instances DROP COLUMN IF EXISTS resource_request;
ALTER TABLE game_instances DROP COLUMN IF EXISTS queued_reason;
ALTER TABLE game_instances DROP COLUMN IF EXISTS queued_at;
ALTER TABLE game_instances DROP COLUMN IF EXISTS cancelled;
