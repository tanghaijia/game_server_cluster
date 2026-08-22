-- 000024：M3 配置下发链路
-- 1) game_container_configs.port_inject_env：端口注入 env 变量名（默认 GAME_HOST_PORT）
-- 2) game_instances.config：实例配置（platform + player 合并，adapter.toml schema 校验后下发）
ALTER TABLE game_container_configs
    ADD COLUMN IF NOT EXISTS port_inject_env TEXT NOT NULL DEFAULT 'GAME_HOST_PORT';

ALTER TABLE game_instances
    ADD COLUMN IF NOT EXISTS config JSONB;
