-- 000030_add_steam_branches_min_replicas.down.sql

ALTER TABLE steam_branches DROP COLUMN IF EXISTS min_replicas;
