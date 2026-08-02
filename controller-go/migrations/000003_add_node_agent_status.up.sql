-- 000003_add_node_agent_status.up.sql
-- node_agents 增加 status 列（NodeAgentStatus: 0=Disabled, 1=Enabled）

ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS status INTEGER;

-- 回填：新增列后，现有 node_agent 默认视为已启用（Enabled=1），
-- 否则调度器按 Enabled 过滤时会把它们全部排除
UPDATE node_agents SET status = 1 WHERE status IS NULL;
