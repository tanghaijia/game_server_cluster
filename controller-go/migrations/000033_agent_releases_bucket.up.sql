-- 000033_agent_releases_bucket.up.sql
-- P2（agent-release-asset-service-redesign）：release 对象存储迁移到 asset_service。
-- 增加 bucket 列（对象所在桶）；storage_key 语义改为对象键 object_key。
ALTER TABLE agent_releases ADD COLUMN IF NOT EXISTS bucket TEXT NOT NULL DEFAULT 'cluster';
