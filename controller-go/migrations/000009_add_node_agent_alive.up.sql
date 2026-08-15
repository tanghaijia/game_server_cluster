-- 000009_add_node_agent_alive.up.sql
-- node_agent 存活检测（controller 心跳探测结果）
ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS alive BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;
