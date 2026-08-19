-- 000012_nodes_resource.up.sql
-- 节点动态资源（node_agent 上报） + 预留（调度事务维护） + 压力状态（3.3 落库供重启恢复）

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS cpu_used_milli          INTEGER  NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS memory_used_bytes       BIGINT   NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS disk_used_bytes         BIGINT   NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS net_rx_bps              BIGINT   NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS net_tx_bps              BIGINT   NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS usage_reported_at       TIMESTAMPTZ;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS cpu_reserved_milli      INTEGER  NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS memory_reserved_bytes   BIGINT   NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS disk_reserved_bytes     BIGINT   NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS pressure_status         INTEGER  NOT NULL DEFAULT 0; -- 0=Normal 1=Warning 2=Critical
