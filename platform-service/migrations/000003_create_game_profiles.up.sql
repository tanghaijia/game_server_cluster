-- 000003_create_game_profiles.up.sql
-- 多游戏平台（见 docs/multi-game-platform-design.md）：
-- 游戏产品/呈现目录，game_id 关联 controller 的 games.ID（跨库，无外键约束）。

CREATE TABLE IF NOT EXISTS game_profiles (
    game_id      TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    icon_url     TEXT NOT NULL DEFAULT '',
    accent_color TEXT NOT NULL DEFAULT '#6366f1',
    description  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order   INT NOT NULL DEFAULT 0,
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 种子数据：当前两个游戏（controller 侧 seed 见 controller-go/migrations/000007/000008）
INSERT INTO game_profiles (game_id, display_name, icon_url, accent_color, description, enabled, sort_order) VALUES
    ('294420', '七日杀', '/static/games/294420.png', '#E63946', '末日生存沙盒：白天探索搜刮，夜晚抵御丧尸潮。', TRUE, 1),
    ('322330', '饥荒联机版', '/static/games/322330.png', '#7C3AED', '哥特风多人生存：一起收集资源，在黑暗降临前活下去。', TRUE, 2)
ON CONFLICT (game_id) DO NOTHING;
