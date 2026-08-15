-- 000011_add_port_injection.up.sql
-- 方案A：端口注入（identity 映射 + 环境变量改写游戏端口）。
--  1) game_container_configs.inject_game_port：该游戏的游戏端口采用"注入模式"——
--     分配宿主端口 H 后，把 H 作为容器内端口（identity 映射），并通过 env 传给 adapter
--     （如 SDTD_SERVER_PORT=H），由 start.sh 改写 serverconfig.xml 的 ServerPort。
--     这样游戏通告的端口 == 宿主映射端口，EOS/Steam 发现与直连一致。
--  2) game_container_port_mappings.is_game_port：标记游戏端口对应的宿主端口行，
--     供 connect 地址查询与 env 构造使用（注入模式下 container_port == host_port，不能按容器端口匹配）。

ALTER TABLE game_container_configs ADD COLUMN IF NOT EXISTS inject_game_port BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE game_container_port_mappings ADD COLUMN IF NOT EXISTS is_game_port BOOLEAN NOT NULL DEFAULT FALSE;

-- 7DTD 启用端口注入（ServerPort 由平台分配，EOS 通告才能指向可达端口）
UPDATE game_container_configs SET inject_game_port = TRUE WHERE id = 'cfg-7dtd-demo';
