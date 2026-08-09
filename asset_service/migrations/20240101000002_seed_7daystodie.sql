-- 20240101000002_seed_7daystodie.sql
-- 预制 7 Days to Die（Steam AppID 294420）专用服务器的 game + build，与 DST 对齐。
-- 幂等：games 用 ON CONFLICT，builds 用 WHERE NOT EXISTS 守卫。

INSERT INTO t_asset_service_games (id, name, app_id)
VALUES ('294420', '7 Days to Die', '294420')
ON CONFLICT (id) DO NOTHING;

INSERT INTO t_asset_service_game_builds (
    build_id, game_id, channel,
    adapter_id, adapter_version_major, adapter_version_minor, adapter_version_patch,
    upstream_version, artifact_uri, artifact_image_name, artifact_image_tag,
    status, pinned, resolved_at, created_at, updated_at
)
SELECT
    '7dtd-public-demo-build', '294420', 'public',
    '7daystodie', 0, 1, 0,
    'demo-upstream', 'ccr.ccs.tencentyun.com/cluster_game_server',
    '7daystodie-adapter', '0.1.0',
    'available', true, NOW(), NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM t_asset_service_game_builds WHERE build_id = '7dtd-public-demo-build'
);
