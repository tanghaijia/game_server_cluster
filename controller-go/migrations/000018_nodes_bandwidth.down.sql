-- 000018_nodes_bandwidth.down.sql

ALTER TABLE nodes DROP COLUMN IF EXISTS net_rx_limit_mbps;
ALTER TABLE nodes DROP COLUMN IF EXISTS net_tx_limit_mbps;
ALTER TABLE nodes DROP COLUMN IF EXISTS bandwidth_rx_reserved_mbps;
ALTER TABLE nodes DROP COLUMN IF EXISTS bandwidth_tx_reserved_mbps;
