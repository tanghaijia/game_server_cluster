-- 000026：M8 外部受限凭证池（credential pool）
-- DST cluster_token 等受限外部资源：管理员手动从官网创建录入池化，
-- 实例启动时分配（in_use）、停止/失败时释放（available）复用。
-- 平台侧全通用：按 game_id + resource_type 池化，游戏专用性只存在于 adapter.toml 声明。
CREATE TABLE IF NOT EXISTS credential_pool (
    id              UUID PRIMARY KEY,
    game_id         TEXT NOT NULL,
    resource_type   TEXT NOT NULL,
    secret          TEXT NOT NULL,          -- 加密存储（后续接入 secret 加密）
    status          TEXT NOT NULL DEFAULT 'available',  -- available / in_use / orphan
    instance_id     TEXT,                   -- 当前占用实例
    last_instance_id TEXT,                  -- 上次占用者（优先复用）
    allocated_at    TIMESTAMPTZ,
    released_at     TIMESTAMPTZ,
    remark          TEXT,
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_credential_pool_game
    ON credential_pool (game_id, resource_type);
CREATE INDEX IF NOT EXISTS idx_credential_pool_instance
    ON credential_pool (instance_id) WHERE instance_id IS NOT NULL;
