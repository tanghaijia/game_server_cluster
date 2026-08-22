-- 适配器元数据/schema 登记表（M4，adapter-framework-design.md §3.6.3）
CREATE TABLE IF NOT EXISTS t_asset_service_adapters (
    adapter_id    TEXT PRIMARY KEY,
    game_id       TEXT NOT NULL,
    -- AdapterMetadata 序列化（port_inject_env + lifecycle 脚本路径）
    metadata_json TEXT NOT NULL DEFAULT '{}',
    -- AdapterSchema 序列化（config settings + i18n 字典）
    schema_json   TEXT NOT NULL DEFAULT '{}',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
