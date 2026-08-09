-- 000006_add_game_app_id.down.sql
ALTER TABLE games DROP COLUMN IF EXISTS app_id;
