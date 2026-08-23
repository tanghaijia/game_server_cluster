-- M13：每订阅实例数量上限
-- server_plans.max_instances：管理员配置（0 = 不限）
-- subscriptions.max_instances：购买时从套餐快照（快照语义：套餐编辑不追溯已购订阅）
ALTER TABLE server_plans ADD COLUMN IF NOT EXISTS max_instances INT NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS max_instances INT NOT NULL DEFAULT 0;
