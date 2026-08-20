-- 000019_scheduling_queue_backfill.up.sql
-- 回填迁移（版本号回溯修复）：
-- 000015 在 P1 时代被跳过（P1 迁移序列为 000012/13/14/16/17，000015 系 P2 新增，
-- golang-migrate 不回溯应用版本号低于已应用版本的迁移），导致旧库缺失 scheduling_queue 表。
-- 本迁移用更高版本号创建同一张表；IF NOT EXISTS 幂等，全新库（000015 已应用）同样安全。

CREATE TABLE IF NOT EXISTS scheduling_queue (
    instance_id TEXT PRIMARY KEY REFERENCES game_instances(id),
    priority    INTEGER NOT NULL DEFAULT 100,
    reason      TEXT,                       -- 排队原因（可读，来自 ScheduleResult.Reason）
    attempts    INTEGER NOT NULL DEFAULT 0, -- 已重试次数（退避依据）
    wake_at     TIMESTAMPTZ NOT NULL,       -- 下次唤醒时间（退避）
    queued_at   TIMESTAMPTZ NOT NULL,
    timeout_at  TIMESTAMPTZ NOT NULL        -- 排队超时截止（S16/D9）
);

CREATE INDEX IF NOT EXISTS idx_scheduling_queue_wake
    ON scheduling_queue (wake_at, priority);
