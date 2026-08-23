DROP INDEX IF EXISTS uq_subscription_single_active;
ALTER TABLE game_instances DROP COLUMN IF EXISTS subscription_id;
