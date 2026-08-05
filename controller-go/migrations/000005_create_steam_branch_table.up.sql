-- 000005_create_steam_branch_table.up.sql
-- 记录 asset_service 同步下来的 Steam 分支信息
CREATE TABLE IF NOT EXISTS steam_branches (
    id            TEXT PRIMARY KEY,
    branch_name   TEXT,
    last_build_id BIGINT,
    description   TEXT,
    game_id       TEXT,
    status        INTEGER,
    create_time   TIMESTAMPTZ,
    update_time   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_steam_branches_game_id
    ON steam_branches (game_id);

CREATE INDEX IF NOT EXISTS idx_steam_branches_game_branch
    ON steam_branches (game_id, branch_name);
