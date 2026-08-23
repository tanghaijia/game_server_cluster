-- M9：用户订阅（购买单元）。一次购买，订阅内可创建多个游戏实例，同一时间至多一个活跃
-- （单活跃约束 M10 在 controller 层落地：game_instances.subscription_id + 部分唯一索引）
CREATE TABLE IF NOT EXISTS subscriptions (
	id               TEXT PRIMARY KEY,
	user_id          TEXT NOT NULL REFERENCES users(id),
	plan_id          TEXT NOT NULL REFERENCES server_plans(id),
	status           TEXT NOT NULL DEFAULT 'active',   -- active / expired / cancelled / suspended
	expires_at       TIMESTAMPTZ,                      -- NULL = 不过期
	basket_snapshot  JSONB NOT NULL DEFAULT '[]',      -- 购买时快照（与套餐编辑解耦）
	create_time      TIMESTAMPTZ NOT NULL DEFAULT now(),
	update_time      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status);
