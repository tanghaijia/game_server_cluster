-- 000032_node_agents_update.down.sql

ALTER TABLE node_agents DROP COLUMN IF EXISTS agent_version;
ALTER TABLE node_agents DROP COLUMN IF EXISTS update_state;
ALTER TABLE node_agents DROP COLUMN IF EXISTS target_version;
ALTER TABLE node_agents DROP COLUMN IF EXISTS last_update_at;
ALTER TABLE node_agents DROP COLUMN IF EXISTS last_update_err;
