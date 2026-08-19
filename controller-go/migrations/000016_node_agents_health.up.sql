-- 000016_node_agents_health.up.sql
-- node_agent 健康状态（9.1）：0=unknown(未探测) 1=healthy 2=degraded 3=unhealthy
-- 首次探测前默认 unknown（修正旧 alive=true 乐观默认，S23）

ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS health_status INTEGER NOT NULL DEFAULT 0;
