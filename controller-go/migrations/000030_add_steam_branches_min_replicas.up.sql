-- 000030_add_steam_branches_min_replicas.up.sql
-- P2-D：分支保底副本数（故障域保底，§4.3）
-- 0 = 不保留（按需，实例驱动）；N = 即使无实例也常驻 N 份（HA/防抖，管理员设）

ALTER TABLE steam_branches ADD COLUMN IF NOT EXISTS min_replicas INT NOT NULL DEFAULT 0;
