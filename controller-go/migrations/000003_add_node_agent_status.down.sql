-- 000003_add_node_agent_status.down.sql
ALTER TABLE node_agents DROP COLUMN IF EXISTS status;
