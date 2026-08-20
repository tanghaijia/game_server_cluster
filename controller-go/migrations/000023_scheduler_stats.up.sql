-- 000023_scheduler_stats.up.sql
-- 调度统计持久化（S29）：调度尝试计数（scheduled/queued/failed）落库，
-- 重启后观测看板/指标不归零（此前仅内存 map，进程重启即清空）。

CREATE TABLE IF NOT EXISTS scheduler_stats (
    outcome TEXT PRIMARY KEY,        -- scheduled / queued / failed
    count   BIGINT NOT NULL DEFAULT 0
);
