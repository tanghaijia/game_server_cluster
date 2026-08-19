-- 000012_nodes_resource.down.sql

ALTER TABLE nodes DROP COLUMN IF EXISTS cpu_used_milli;
ALTER TABLE nodes DROP COLUMN IF EXISTS memory_used_bytes;
ALTER TABLE nodes DROP COLUMN IF EXISTS disk_used_bytes;
ALTER TABLE nodes DROP COLUMN IF EXISTS net_rx_bps;
ALTER TABLE nodes DROP COLUMN IF EXISTS net_tx_bps;
ALTER TABLE nodes DROP COLUMN IF EXISTS usage_reported_at;
ALTER TABLE nodes DROP COLUMN IF EXISTS cpu_reserved_milli;
ALTER TABLE nodes DROP COLUMN IF EXISTS memory_reserved_bytes;
ALTER TABLE nodes DROP COLUMN IF EXISTS disk_reserved_bytes;
ALTER TABLE nodes DROP COLUMN IF EXISTS pressure_status;
