-- 000025：平台运营方配置（M5，adapter-framework-design.md §3.3.4）
-- control=platform 的配置项按游戏全局存储；创建/启动实例时与 player 配置合并下发
CREATE TABLE IF NOT EXISTS game_platform_configs (
    game_id     TEXT PRIMARY KEY,
    config      JSONB NOT NULL DEFAULT '{}',
    version     BIGINT NOT NULL DEFAULT 0,
    updated_by  TEXT NOT NULL DEFAULT '',
    update_time TIMESTAMPTZ NOT NULL DEFAULT now()
);
