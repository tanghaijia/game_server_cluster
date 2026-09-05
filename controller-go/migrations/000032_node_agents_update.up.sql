-- 000032_node_agents_update.up.sql
-- node_agent 一键更新：版本自述 + 更新状态机（docs/node-agent-upgrade-design.md §3.3）
-- update_state: idle | downloading | verifying | rebooting | updated | failed
-- target_version: 目标发布版本（更新中）；last_update_err: 最近一次失败原因

ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS agent_version   TEXT NOT NULL DEFAULT '';
ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS update_state    TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS target_version  TEXT NOT NULL DEFAULT '';
ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS last_update_at  TIMESTAMPTZ;
ALTER TABLE node_agents ADD COLUMN IF NOT EXISTS last_update_err TEXT NOT NULL DEFAULT '';
