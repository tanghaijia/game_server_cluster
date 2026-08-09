-- 000006_add_game_app_id.up.sql
-- Game 增加 app_id，存量行 app_id 拷贝 id（兼容已存在的游戏）
ALTER TABLE games ADD COLUMN IF NOT EXISTS app_id TEXT;

UPDATE games
SET app_id = id
WHERE app_id IS NULL OR app_id = '';
