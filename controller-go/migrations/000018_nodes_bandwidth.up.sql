-- 000018_nodes_bandwidth.up.sql
-- 节点带宽（§3.5 带宽评分，D6）：上限（平台可分配）+ 预留（调度事务维护，与 cpu/mem/disk 同构）。
-- 带宽为软约束（D6）：不参与硬约束排除，仅评分（bandwidth_score）。

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS net_rx_limit_mbps        INTEGER NOT NULL DEFAULT 1000;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS net_tx_limit_mbps        INTEGER NOT NULL DEFAULT 1000;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS bandwidth_rx_reserved_mbps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS bandwidth_tx_reserved_mbps INTEGER NOT NULL DEFAULT 0;
