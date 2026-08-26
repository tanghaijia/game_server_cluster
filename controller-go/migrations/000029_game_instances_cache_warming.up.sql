-- 000029_game_instances_cache_warming.up.sql
-- P2-C：实例落库解析出的 Steam 分支 + 目标缓存 buildid
-- （cache_warming 状态：选中节点无缓存时等下载完成；demand 统计按分支聚合）

ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS branch_name TEXT NOT NULL DEFAULT '';
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS cache_build_id TEXT NOT NULL DEFAULT '';
