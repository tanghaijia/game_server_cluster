-- 000007_seed_7daystodie.down.sql
-- 撤销 7 Days to Die 预制数据（按依赖逆序删除）
DELETE FROM games WHERE id = '294420';
DELETE FROM game_container_port_excerpts WHERE game_container_config_id = 'cfg-7dtd-demo';
DELETE FROM game_container_configs WHERE id = 'cfg-7dtd-demo';
