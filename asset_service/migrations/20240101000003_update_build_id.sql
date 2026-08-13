-- 修复 38a5300 遗留：把旧种子 build_id 升级到 {game_id}-{channel}-{image_tag} 规则
-- 对应提交 38a5300 直接修改已执行迁移文件的 checksum 冲突（VersionMismatch）
UPDATE t_asset_service_game_builds SET build_id = '343050-public-0.2.2' WHERE build_id = 'dst-public-demo-build';
UPDATE t_asset_service_game_builds SET build_id = '294420-public-0.1.0' WHERE build_id = '7dtd-public-demo-build';
