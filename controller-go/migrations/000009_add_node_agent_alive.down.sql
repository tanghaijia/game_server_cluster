-- 000009_add_node_agent_alive.down.sql
ALTER TABLE node_agents DROP COLUMN IF EXISTS last_heartbeat_at;
ALTER TABLE node_agents DROP COLUMN IF EXISTS alive;
