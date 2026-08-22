ALTER TABLE game_container_configs
    DROP COLUMN IF EXISTS port_inject_env;

ALTER TABLE game_instances
    DROP COLUMN IF EXISTS config;
