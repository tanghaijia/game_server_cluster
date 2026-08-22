-- 收敛：适配器元数据/schema 随 GameBuild 存储（删除独立 adapter 实体表）
-- 1) t_asset_service_game_builds 加 metadata_json / schema_json
-- 2) 旧 adapter 实体表保留但不再写入（可手动清理）
ALTER TABLE t_asset_service_game_builds
    ADD COLUMN IF NOT EXISTS metadata_json TEXT NOT NULL DEFAULT '{}';

ALTER TABLE t_asset_service_game_builds
    ADD COLUMN IF NOT EXISTS schema_json TEXT;
