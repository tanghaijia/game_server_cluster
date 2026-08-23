-- 000027：M10 订阅单活跃约束（见 docs/subscription-design.md §4.3/§5）
-- subscription_id：实例归属的订阅（可空）。老实例/未归属实例为 NULL，
-- 部分唯一索引天然忽略 NULL → 自动豁免，无需数据迁移。
ALTER TABLE game_instances ADD COLUMN IF NOT EXISTS subscription_id TEXT;

-- 部分唯一索引：同一订阅至多一个"活跃"实例。
-- 活跃 = 一切非终态 = status NOT IN (stopped=8, failed=10)。
-- ⚠️ 谓词中的状态编号必须与 entity.InstanceStatus.IsActive() 保持一致：
--    防漂移测试（internal/entity/game_instance_test.go）解析本文件校验，改枚举必须同步这里。
CREATE UNIQUE INDEX IF NOT EXISTS uq_subscription_single_active
ON game_instances (subscription_id)
WHERE subscription_id IS NOT NULL
  AND status NOT IN (8, 10);
