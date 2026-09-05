-- 000033_agent_releases_bucket.down.sql
ALTER TABLE agent_releases DROP COLUMN IF EXISTS bucket;
