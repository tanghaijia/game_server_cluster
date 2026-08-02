-- 000002_seed_data.down.sql
-- 撤销预制数据（按依赖逆序删除）
DELETE FROM node_agents WHERE id = 'node-agent-1';
DELETE FROM nodes WHERE id = 1;
DELETE FROM games WHERE id = '343050';
DELETE FROM game_container_port_mappings WHERE game_container_config_id = 'cfg-dst-demo';
DELETE FROM game_container_configs WHERE id = 'cfg-dst-demo';
