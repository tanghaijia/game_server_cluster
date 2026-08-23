-- M9：管理员套餐（SKU）。篮子 basket = 允许的游戏 + 每游戏默认配置 preset
-- （购买时快照进 subscriptions.basket_snapshot，套餐编辑不影响已购订阅）
CREATE TABLE IF NOT EXISTS server_plans (
	id                     TEXT PRIMARY KEY,
	display_name           TEXT NOT NULL,
	description            TEXT NOT NULL DEFAULT '',
	price_cents            BIGINT NOT NULL DEFAULT 0,     -- 金额：分（整数，避免浮点误差）
	duration_hours         INT NOT NULL DEFAULT 0,        -- 时长：小时（0 = 永久/手动）
	resource_cpu_milli     INT NOT NULL DEFAULT 0,        -- 资源上限提示（真实调度仍按实例需求）
	resource_memory_bytes  BIGINT NOT NULL DEFAULT 0,
	resource_disk_bytes    BIGINT NOT NULL DEFAULT 0,
	basket                 JSONB NOT NULL DEFAULT '[]',   -- [{game_id, config:{...}}]
	enabled                BOOLEAN NOT NULL DEFAULT TRUE,
	create_time            TIMESTAMPTZ NOT NULL DEFAULT now(),
	update_time            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_server_plans_enabled ON server_plans(enabled);
