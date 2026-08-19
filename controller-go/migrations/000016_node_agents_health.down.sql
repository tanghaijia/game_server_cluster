-- 000016_node_agents_health.down.sql

ALTER TABLE node_agents DROP COLUMN IF EXISTS health_status;
