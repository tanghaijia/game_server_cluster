-- 000020_game_instances_fail_reason.up.sql
-- 实例失败原因（观测/排障）：调度失败、阶段失败、排队超时、卡死哨兵等写入，
-- 前端实例视图展示"为什么失败"（此前仅 status=failed，无原因）。

ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS fail_reason TEXT;
