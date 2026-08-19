-- 000013_node_resource_samples.up.sql
-- 节点资源采样（历史视图数据源，3.4）：评分（均值/P95）与压力状态机（持续观测）共用

CREATE TABLE IF NOT EXISTS node_resource_samples (
    id                BIGSERIAL PRIMARY KEY,
    node_id           TEXT NOT NULL,
    sampled_at        TIMESTAMPTZ NOT NULL,
    cpu_used_milli    INTEGER NOT NULL,
    memory_used_bytes BIGINT NOT NULL,
    disk_used_bytes   BIGINT NOT NULL,
    net_rx_bps        BIGINT NOT NULL,
    net_tx_bps        BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_node_resource_samples_node_time
    ON node_resource_samples (node_id, sampled_at);
