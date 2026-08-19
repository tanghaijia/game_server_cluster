-- 000017_game_container_configs_resources.up.sql
-- 游戏容器配置的资源默认值（3.1 来源优先级第二层，D8）
-- single_threaded：单核应用声明（3.1 声明规范），调度校验整核 + 启用单核主频评分

ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS cpu_request_milli      INTEGER NOT NULL DEFAULT 1000;
ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS memory_request_bytes   BIGINT  NOT NULL DEFAULT 1073741824;
ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS disk_request_bytes     BIGINT  NOT NULL DEFAULT 10737418240;
ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS bandwidth_rx_mbps      INTEGER NOT NULL DEFAULT 50;
ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS bandwidth_tx_mbps      INTEGER NOT NULL DEFAULT 50;
ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS single_threaded       BOOLEAN NOT NULL DEFAULT FALSE;
