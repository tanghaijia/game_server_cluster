-- 000022_scheduler_events.up.sql
-- 调度事件持久化（观测/审计，S30）：调度器/队列/压力/健康等发布的事件落库，
-- 重启后管理员仍可回溯"谁被调度到哪、为什么排队/失败"。内存缓冲只做实时展示，
-- DB 为历史来源（保留策略：默认 7 天，由 controller 定期清理）。

CREATE TABLE IF NOT EXISTS scheduler_events (
    id           BIGSERIAL PRIMARY KEY,
    occurred_at  TIMESTAMPTZ NOT NULL,
    type         TEXT NOT NULL,        -- instance_scheduled / instance_queued / ...
    instance_id  TEXT,
    node_agent_id TEXT,
    detail       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_scheduler_events_time ON scheduler_events (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_scheduler_events_type_time ON scheduler_events (type, occurred_at DESC);
