-- 000015_scheduling_queue.up.sql
-- 排队队列（R8/D4，§8.1）：一行 = 一个排队实例；game_instances.status=Queued 与队列行一一对应。
-- 排序：priority ASC, queued_at ASC（数值越小越优先，默认 100，D7）。

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
