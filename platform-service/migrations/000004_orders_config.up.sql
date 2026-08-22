-- 000004：订单携带实例配置（M5）
-- 下单时保存 config（游戏配置 schema 声明的键值），支付/开服时透传给 controller
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS config JSONB;
