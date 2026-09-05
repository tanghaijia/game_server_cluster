-- 000031_agent_releases.up.sql
-- node_agent 版本发布清单（docs/node-agent-upgrade-design.md §3.1）
-- 上传的 node_agent 二进制元数据；文件本体由 ReleaseStore 落盘（默认 controller 本地目录）

CREATE TABLE IF NOT EXISTS agent_releases (
    id          TEXT PRIMARY KEY,
    version     TEXT NOT NULL,             -- 如 v0.1.1
    os          TEXT NOT NULL DEFAULT 'linux',  -- linux / windows
    arch        TEXT NOT NULL DEFAULT 'amd64',  -- amd64 / arm64
    sha256      TEXT NOT NULL,             -- 文件完整性哈希（登记时计算 + 下载后复核）
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    storage_key TEXT NOT NULL,             -- ReleaseStore 内唯一键（如 release-<id>-<version>）
    note        TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_releases_version ON agent_releases (version);
