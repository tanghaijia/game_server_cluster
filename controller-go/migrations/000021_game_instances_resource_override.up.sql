-- 000021_game_instances_resource_override.up.sql
-- 实例资源需求显式覆盖标记（配置优先级修复）：
-- instance.resource_request 有两个来源——① 创建时用户显式指定（应覆盖 config 默认）；
-- ② 调度成功后写回的解析快照（仅用于释放预留，不应覆盖后续 config 变更）。
-- 本标记区分两者：resource_override=true 表示创建时显式指定，调度时覆盖 config；
-- false（调度写回快照）则每次调度以 config 当前值为准。

ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS resource_override BOOLEAN NOT NULL DEFAULT FALSE;
