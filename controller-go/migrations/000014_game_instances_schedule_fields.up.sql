-- 000014_game_instances_schedule_fields.up.sql
-- 实例调度字段：region（R3）、priority（D7）、resource_request（3.1）
-- queued_reason/queued_at/cancelled 为排队与取消字段（R8/D10，P2 使用，一次建全避免反复迁移）

ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS region           TEXT;
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS priority         INTEGER NOT NULL DEFAULT 100;
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS resource_request JSONB;
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS queued_reason    TEXT;
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS queued_at        TIMESTAMPTZ;
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS cancelled        BOOLEAN NOT NULL DEFAULT FALSE;
